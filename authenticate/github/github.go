package github

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/tweag/credential-helper/api"
	"golang.org/x/oauth2"
	"sigs.k8s.io/yaml"
)

type GitHub struct {
	tokenSource oauth2.TokenSource
}

func New(ctx context.Context) (*GitHub, error) {
	if tokenSource, err := NewGitHubTokenSourceFromEnv(); err == nil {
		return &GitHub{tokenSource: tokenSource}, nil
	}
	hosts, err := hostsFromFile(path.Join(configDir(), "hosts.yml"))
	if err != nil {
		return nil, err
	}
	tokenSource, err := NewGitHubTokenSource("github.com", hosts)
	if err != nil {
		return nil, err
	}
	return &GitHub{tokenSource: tokenSource}, nil
}

// Get implements the get command of the credential-helper spec:
//
// https://github.com/EngFlow/credential-helper-spec/blob/main/spec.md#get
func (g *GitHub) Get(ctx context.Context, req api.GetCredentialsRequest) (api.GetCredentialsResponse, error) {
	parsedURL, error := url.Parse(req.URI)
	if error != nil {
		return api.GetCredentialsResponse{}, error
	}

	if parsedURL.Scheme != "https" {
		return api.GetCredentialsResponse{}, errors.New("only https is supported")
	}

	switch strings.ToLower(parsedURL.Host) {
	case "github.com", "objects.githubusercontent.com":
		// this is fine
	default:
		return api.GetCredentialsResponse{}, errors.New("only github.com and objects.githubusercontent.com are supported")
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

// CacheKey returns a cache key for the given request.
// For GitHub, the same token can be used for all requests.
func (g *GitHub) CacheKey(req api.GetCredentialsRequest) string {
	parsedURL, error := url.Parse(req.URI)
	if error != nil {
		return "" // disable caching
	}
	return parsedURL.Host
}

type ghCLITokenSource struct{}

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
	token string
}

func NewGitHubTokenSourceFromEnv() (*GitHubTokenSource, error) {
	token, ok := os.LookupEnv("GH_TOKEN")
	if ok {
		return &GitHubTokenSource{token: token}, nil
	}
	token, ok = os.LookupEnv("GITHUB_TOKEN")
	if ok {
		return &GitHubTokenSource{token: token}, nil
	}
	return nil, fmt.Errorf("no token found in environment")
}

func NewGitHubTokenSource(host string, cfg HostsFile) (*GitHubTokenSource, error) {
	if hostCfg, ok := cfg[host]; ok {
		return &GitHubTokenSource{token: hostCfg.OAuthToken}, nil
	}
	return nil, fmt.Errorf("no token found for host %q", host)
}

func (g *GitHubTokenSource) Token() (*oauth2.Token, error) {
	return &oauth2.Token{
		AccessToken: g.token,
		// TODO: guess or check the expiry time
		// TODO: add method to reload token from disk
	}, nil
}

type HostsFile map[string]HostConfig

type HostConfig struct {
	OAuthToken string `json:"oauth_token"`
}
