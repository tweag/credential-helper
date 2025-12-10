package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/tweag/credential-helper/api"
	"github.com/tweag/credential-helper/authenticate/internal/helperconfig"
	"github.com/tweag/credential-helper/authenticate/internal/lookupchain"
	"github.com/tweag/credential-helper/logging"
	"golang.org/x/oauth2"
	"sigs.k8s.io/yaml"
)

type GitHub struct{}

func (g *GitHub) Resolver(ctx context.Context) (api.Resolver, error) {
	cfg, err := configFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return &GitHubResolver{tokenSource: &GitHubTokenSource{config: cfg}}, nil
}

func (g *GitHub) SetupInstructionsForURI(ctx context.Context, uri string) string {
	var lookupChainInstructions string
	cfg, err := configFromContext(ctx)
	if err == nil {
		chain := lookupchain.New(cfg.LookupChain)
		lookupChainInstructions = chain.SetupInstructions("default", "secret sent to GitHub as a bearer token in the Authorization header")
	} else {
		lookupChainInstructions = fmt.Sprintf("due to a configuration parsing issue, no further setup instructions are available: %v", err)
	}

	return fmt.Sprintf(`%s is a GitHub url.

The credential helper can be used to download any assets GitHub hosts, including:

  - the git protocol via https
  - raw code files (raw.githubusercontent.com/<org>/<repo>/<commit>/<file>)
  - patches (github.com/<org>/<repo>/<commit>.patch)
  - source tarballs (github.com/<org>/<repo>/archive/refs/tags/v1.2.3.tar.gz)
  - release assets (github.com/<org>/<repo>/releases/download/v1.2.3/<file>)
  - container images from ghcr.io (doc)
  ... and more.

With credentials, you are also less likely to be blocked by GitHub rate limits, even when accessing public repositories.

Authentication Methods:

  Option 1: Using the GitHub CLI as a regular user (Recommended)
    1. Install the GitHub CLI (gh): https://github.com/cli/cli#installation
    2. Login via:
      $ gh auth login
    3. Follow the browser prompts to authenticate

  Option 2: Authentication using a GitHub App, GitHub Actions Token or Personal Access Token (PAT)
    1. Setup your authentication method of choice
    2. Set the required environment variable (GH_TOKEN or GITHUB_TOKEN) when running Bazel (or other tools that access credential helpers)
    3. Alternatively, add the secret to the system keyring under the gh:github.com key

%s`, uri, lookupChainInstructions)
}

// CacheKey returns a cache key for the given request.
// For GitHub, the same token can be used for all requests.
func (g *GitHub) CacheKey(req api.GetCredentialsRequest) string {
	parsedURL, error := url.Parse(req.URI)
	if error != nil {
		return "" // disable caching
	}
	return parsedURL.Host
}

type GitHubResolver struct {
	tokenSource oauth2.TokenSource
}

// Get implements the get command of the credential-helper spec:
//
// https://github.com/EngFlow/credential-helper-spec/blob/main/spec.md#get
func (g *GitHubResolver) Get(ctx context.Context, req api.GetCredentialsRequest) (api.GetCredentialsResponse, error) {
	parsedURL, error := url.Parse(req.URI)
	if error != nil {
		return api.GetCredentialsResponse{}, error
	}

	if parsedURL.Scheme != "https" {
		return api.GetCredentialsResponse{}, errors.New("only https is supported")
	}

	switch {
	case strings.EqualFold(parsedURL.Host, "github.com"):
		// this is fine
	case strings.HasSuffix(strings.ToLower(parsedURL.Host), ".github.com"):
		// this is fine
	case strings.EqualFold(parsedURL.Host, "raw.githubusercontent.com"):
		// this is fine
	default:
		return api.GetCredentialsResponse{}, errors.New("only github.com and subdomains are supported")
	}

	token, err := g.tokenSource.Token()
	if err != nil {
		return api.GetCredentialsResponse{}, err
	}
	var expires string
	if !token.Expiry.IsZero() {
		expires = token.Expiry.UTC().Format(time.RFC3339)
	}
	return api.GetCredentialsResponse{
		Expires: expires,
		Headers: map[string][]string{
			"Authorization": {"Bearer " + token.AccessToken},
		},
	}, nil
}

// path precedence: GH_CONFIG_DIR, XDG_CONFIG_HOME, AppData (windows only), HOME.
func configDir() string {
	var path string
	if a := os.Getenv("GH_CONFIG_DIR"); len(a) > 0 {
		path = a
	} else if b := os.Getenv("XDG_CONFIG_HOME"); b != "" {
		path = filepath.Join(b, "gh")
	} else if c := os.Getenv("AppData"); runtime.GOOS == "windows" && c != "" {
		path = filepath.Join(c, "GitHub CLI")
	} else {
		d, _ := os.UserHomeDir()
		path = filepath.Join(d, ".config", "gh")
	}
	return path
}

func hostsFromFile(path string) (HostsFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var hosts HostsFile
	if err := yaml.Unmarshal(raw, &hosts); err != nil {
		return nil, err
	}
	return hosts, nil
}

type GitHubTokenSource struct {
	config configFragment
}

func (g *GitHubTokenSource) Token() (*oauth2.Token, error) {
	chain := lookupchain.New(g.config.LookupChain)
	token, err := chain.Lookup("default")
	if err != nil && g.config.ReadConfigFile {
		logging.Debugf("no token found in lookup chain - falling back to GitHub config file: %v", err)
		const host = "github.com"
		cfg, err := hostsFromFile(filepath.Join(configDir(), "hosts.yml"))
		if err != nil {
			return nil, err
		}
		hostCfg, ok := cfg[host]
		if !ok || hostCfg.OAuthToken == "" {
			return nil, errors.New("no token available")
		}
		token = hostCfg.OAuthToken
	} else if err != nil {
		return nil, err
	}

	return &oauth2.Token{
		AccessToken: token,
		Expiry:      g.checkTokenExpiration(token),
		// TODO: add method to reload token from disk
		// in case this token is known to have expired
	}, nil
}

// checkTokenExpiration uses the `/rate_limit` api endpoint to
// query for the token expiration.
// May return zero time if this information is not provided.
func (g *GitHubTokenSource) checkTokenExpiration(token string) time.Time {
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/rate_limit", http.NoBody)
	if err != nil {
		return time.Time{}
	}
	req.Header["Authorization"] = []string{"Bearer " + token}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return time.Time{}
	}
	expirationStr := resp.Header.Get("GitHub-Authentication-Token-Expiration")
	if expiration, err := time.Parse("2006-01-02 03:04:05 -0700", expirationStr); err == nil {
		return expiration.UTC()
	}
	// fallback to unknown expiration
	// since this response header is not provided for
	// every kind of token
	// (and some do not expire at all, unless manually revoked)
	return time.Time{}
}

type HostsFile map[string]HostConfig

type HostConfig struct {
	OAuthToken string `json:"oauth_token"`
}

type configFragment struct {
	LookupChain    lookupchain.Config `json:"lookup_chain"`
	ReadConfigFile bool               `json:"read_config_file"`
}

func configFromContext(ctx context.Context) (configFragment, error) {
	return helperconfig.FromContext(ctx, configFragment{
		LookupChain: lookupchain.Default([]lookupchain.Source{
			&lookupchain.Env{
				Source:  "env",
				Name:    "GH_TOKEN",
				Binding: "default",
			},
			&lookupchain.Env{
				Source:  "env",
				Name:    "GITHUB_TOKEN",
				Binding: "default",
			},
			&lookupchain.Keyring{
				Source:  "keyring",
				Service: "gh:github.com",
				Binding: "default",
			},
		}),
		ReadConfigFile: true,
	})
}
