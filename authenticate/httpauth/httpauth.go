// Package httpauth sends a static credential (HTTP Basic auth, a bearer token
// or a raw header value) for plain HTTP(S) downloads, such as Artifactory,
// Nexus, Artifact Keeper or Chainguard's apk and Libraries repositories.
package httpauth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/tweag/credential-helper/api"
	"github.com/tweag/credential-helper/authenticate/internal/helperconfig"
	"github.com/tweag/credential-helper/authenticate/internal/lookupchain"
)

const (
	AuthMethodBasic  = "basic_auth"
	AuthMethodBearer = "bearer"
	AuthMethodHeader = "header"
)

type configFragment struct {
	// AuthMethod is how the secret is sent:
	//   - "basic_auth" (default): the secret is "<user>:<password>" and is sent
	//     as "Authorization: Basic <base64>".
	//   - "bearer": the secret is sent as "Authorization: Bearer <secret>".
	//   - "header": the secret is sent verbatim in the header named by HeaderName.
	AuthMethod string `json:"auth_method"`
	// HeaderName is the header to set for the "header" auth method.
	HeaderName string `json:"header_name"`
	// AllowMissing sends no header, instead of failing, when the lookup chain
	// finds no secret. Use it for a pull-through cache listed ahead of the
	// origin URL: a machine without the credential gets a 401 from the cache
	// and Bazel moves on to the next URL.
	AllowMissing bool `json:"allow_missing"`
	// LookupChain finds the "default" secret binding.
	LookupChain lookupchain.Config `json:"lookup_chain"`
}

// wellKnownHost holds the defaults and setup instructions for a host that
// needs no configuration.
type wellKnownHost struct {
	defaults     configFragment
	instructions func(u *url.URL) string
}

func envDefault(name string) lookupchain.Config {
	return lookupchain.Default([]lookupchain.Source{
		&lookupchain.Env{Source: lookupchain.SourceEnv, Name: name, Binding: "default"},
	})
}

// chainguardInstructions explains Chainguard pull tokens. Each "chainctl auth
// pull-token" invocation mints a new identity, so the identity id (the Basic
// username) and the token rotate together and are stored as one value.
func chainguardInstructions(repo string) string {
	return fmt.Sprintf(`The credential is a Chainguard pull token, sent as HTTP Basic auth. Because
each "chainctl auth pull-token" invocation mints a new identity, the token's
identity id and the token itself rotate together, so store them as a single
"<identity-id>:<token>" value. Mint it with:

    chainctl auth pull-token --repository=%s --output=env --parent=<ORG>
    # then store "<identity-id>:<token>" from the variables it prints`, repo)
}

var wellKnownHosts = map[string]wellKnownHost{
	"apk.cgr.dev": {
		defaults: configFragment{AuthMethod: AuthMethodBasic, LookupChain: envDefault("CGR_APK_CREDENTIAL")},
		instructions: func(*url.URL) string {
			return "apk.cgr.dev is a private Chainguard apk repository.\n\n" + chainguardInstructions("apk")
		},
	},
	"libraries.cgr.dev": {
		defaults: configFragment{AuthMethod: AuthMethodBasic, LookupChain: envDefault("CGR_LIBRARIES_CREDENTIAL")},
		instructions: func(u *url.URL) string {
			repo := strings.TrimSuffix(firstPathSegment(u), "-upstream")
			if repo == "" {
				repo = "<ecosystem>"
			}
			return "libraries.cgr.dev is Chainguard Libraries; each ecosystem (javascript, java, python, go) takes its own pull token.\n\n" + chainguardInstructions(repo)
		},
	},
}

func defaultConfig(u *url.URL) configFragment {
	if known, ok := wellKnownHosts[strings.ToLower(u.Hostname())]; ok {
		return known.defaults
	}
	return configFragment{
		AuthMethod: AuthMethodBasic,
		LookupChain: lookupchain.Default([]lookupchain.Source{
			&lookupchain.Env{Source: lookupchain.SourceEnv, Name: "CREDENTIAL_HELPER_HTTP_SECRET", Binding: "default"},
			&lookupchain.Keyring{Source: lookupchain.SourceKeyring, Service: "tweag-credential-helper:http", Binding: "default"},
		}),
	}
}

func configFromContext(ctx context.Context, u *url.URL) (configFragment, error) {
	return helperconfig.FromContext(ctx, defaultConfig(u))
}

func firstPathSegment(u *url.URL) string {
	segment, _, _ := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	return segment
}

// HTTP authenticates plain HTTP(S) downloads with a static secret.
type HTTP struct{}

func (h *HTTP) Resolver(ctx context.Context) (api.Resolver, error) {
	return h, nil
}

// CacheKey returns the host and the first path segment. A server often takes
// a different credential per repository at the top of the path (Chainguard
// Libraries per ecosystem, Artifactory per repository key), configured as
// separate path-scoped entries, so a secret cached for one must not be
// served for another.
func (h *HTTP) CacheKey(req api.GetCredentialsRequest) string {
	u, err := url.Parse(req.URI)
	if err != nil {
		return ""
	}
	return u.Host + "/" + firstPathSegment(u)
}

func (h *HTTP) SetupInstructionsForURI(ctx context.Context, uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		u = &url.URL{}
	}
	cfg, err := configFromContext(ctx, u)
	if err != nil {
		return fmt.Sprintf("%s: configuration error: %v", uri, err)
	}
	intro := fmt.Sprintf("%s is authenticated with a static secret (auth method %q).", uri, cfg.AuthMethod)
	if known, ok := wellKnownHosts[strings.ToLower(u.Hostname())]; ok {
		intro = known.instructions(u)
	}
	chain := lookupchain.New(cfg.LookupChain)
	return intro + "\n\n" + chain.SetupInstructions("default", fmt.Sprintf("secret sent with auth method %q", cfg.AuthMethod))
}

func (h *HTTP) Get(ctx context.Context, req api.GetCredentialsRequest) (api.GetCredentialsResponse, error) {
	u, err := url.Parse(req.URI)
	if err != nil {
		return api.GetCredentialsResponse{}, err
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return api.GetCredentialsResponse{}, fmt.Errorf("only http and https are supported, got %q", u.Scheme)
	}
	cfg, err := configFromContext(ctx, u)
	if err != nil {
		return api.GetCredentialsResponse{}, fmt.Errorf("getting configuration fragment for http helper and url %s: %w", req.URI, err)
	}
	return respond(cfg, func() (string, error) { return lookupchain.New(cfg.LookupChain).Lookup("default") })
}

func respond(cfg configFragment, lookup func() (string, error)) (api.GetCredentialsResponse, error) {
	secret, err := lookup()
	missing := lookupchain.IsNotFoundErr(err) || (err == nil && secret == "")
	if missing && cfg.AllowMissing {
		return api.GetCredentialsResponse{}, nil
	}
	if err != nil {
		return api.GetCredentialsResponse{}, err
	}
	if secret == "" {
		return api.GetCredentialsResponse{}, errors.New("the lookup chain returned an empty secret")
	}

	var name, value string
	switch cfg.AuthMethod {
	case AuthMethodBasic, "":
		if _, _, ok := strings.Cut(secret, ":"); !ok {
			return api.GetCredentialsResponse{}, errors.New(`auth method "basic_auth" needs a secret of the form "<user>:<password>"`)
		}
		name, value = "Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(secret))
	case AuthMethodBearer:
		name, value = "Authorization", "Bearer "+secret
	case AuthMethodHeader:
		if cfg.HeaderName == "" {
			return api.GetCredentialsResponse{}, errors.New(`header_name must be set for auth method "header"`)
		}
		name, value = cfg.HeaderName, secret
	default:
		return api.GetCredentialsResponse{}, fmt.Errorf(`unknown auth method %q; possible values are "basic_auth", "bearer" and "header"`, cfg.AuthMethod)
	}
	return api.GetCredentialsResponse{
		Headers: map[string][]string{name: {value}},
	}, nil
}
