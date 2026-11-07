package chainguardapk

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

	"github.com/tweag/credential-helper/api"
	"github.com/tweag/credential-helper/authenticate/internal/helperconfig"
	"github.com/tweag/credential-helper/authenticate/internal/lookupchain"
)

const (
	apkHost       = "apk.cgr.dev"
	librariesHost = "libraries.cgr.dev"
)

type configFragment struct {
	LookupChain lookupchain.Config `json:"lookup_chain"`
}

// ChainguardAPK authenticates to one Chainguard registry host with a pull
// token sent as HTTP Basic auth. The zero value serves the apk repository
// (apk.cgr.dev); NewLibraries serves Chainguard Libraries (libraries.cgr.dev),
// which takes the same kind of credential.
type ChainguardAPK struct {
	host    string
	envName string
	kind    string
	repo    string
}

// NewLibraries returns a helper for the Chainguard Libraries registry
// (libraries.cgr.dev), which serves the javascript, python, java and go
// repositories.
func NewLibraries() *ChainguardAPK {
	return &ChainguardAPK{
		host:    librariesHost,
		envName: "CGR_LIBRARIES_CREDENTIAL",
		kind:    "Chainguard Libraries",
		repo:    "javascript",
	}
}

func (c *ChainguardAPK) settings() (host, envName, kind, repo string) {
	if c.host == "" {
		return apkHost, "CGR_APK_CREDENTIAL", "Chainguard apk", "apk"
	}
	return c.host, c.envName, c.kind, c.repo
}

func (c *ChainguardAPK) configFromContext(ctx context.Context) (configFragment, error) {
	_, envName, _, _ := c.settings()
	return helperconfig.FromContext(ctx, configFragment{
		LookupChain: lookupchain.Default([]lookupchain.Source{
			&lookupchain.Env{
				Source:  lookupchain.SourceEnv,
				Name:    envName,
				Binding: "default",
			},
		}),
	})
}

func (c *ChainguardAPK) Resolver(ctx context.Context) (api.Resolver, error) {
	cfg, err := c.configFromContext(ctx)
	if err != nil {
		return nil, err
	}
	chain := lookupchain.New(cfg.LookupChain)
	host, _, _, _ := c.settings()
	return &ChainguardAPKResolver{
		host:       host,
		credential: func(context.Context) (string, error) { return chain.Lookup("default") },
	}, nil
}

// CacheKey returns the host, plus the first path segment for Chainguard
// Libraries. libraries.cgr.dev serves each ecosystem (javascript, java, go, ...)
// as a top-level path, and each takes its own pull token, configured as a
// separate path-scoped entry in .tweag-credential-helper.json, so a credential
// cached for one repository must not be served for another.
func (c *ChainguardAPK) CacheKey(req api.GetCredentialsRequest) string {
	u, err := url.Parse(req.URI)
	if err != nil {
		return ""
	}
	if c.host != librariesHost {
		return u.Host
	}
	repo, _, _ := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	return u.Host + "/" + repo
}

func (c *ChainguardAPK) SetupInstructionsForURI(ctx context.Context, uri string) string {
	cfg, err := c.configFromContext(ctx)
	if err != nil {
		return fmt.Sprintf("%s: configuration error: %v", uri, err)
	}
	chain := lookupchain.New(cfg.LookupChain)
	_, _, kind, repo := c.settings()
	if c.host == librariesHost {
		if u, err := url.Parse(uri); err == nil {
			if r, _, _ := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/"); r != "" {
				repo = strings.TrimSuffix(r, "-upstream")
			}
		}
	}
	return fmt.Sprintf(`%s is a %s repository.

The credential helper authenticates package downloads using a Chainguard pull
token, sent as HTTP Basic auth. Because each "chainctl auth pull-token" invocation
mints a new identity, the pull token's identity id and the token itself rotate
together, so the credential is stored as a single "<identity-id>:<token>" string
(the HTTP Basic user:password userinfo). Mint it with:

    chainctl auth pull-token --repository=%s --output=env --parent=<ORG>
    # then store "<identity-id>:<token>" from the variables it prints

%s`, uri, kind, repo, chain.SetupInstructions("default", kind+" <identity-id>:<token> sent as HTTP Basic auth"))
}

type ChainguardAPKResolver struct {
	// host is the only host the resolver sends the credential to. Empty means
	// apk.cgr.dev.
	host       string
	credential func(context.Context) (string, error)
}

func (r *ChainguardAPKResolver) Get(ctx context.Context, req api.GetCredentialsRequest) (api.GetCredentialsResponse, error) {
	u, err := url.Parse(req.URI)
	if err != nil {
		return api.GetCredentialsResponse{}, err
	}
	host := r.host
	if host == "" {
		host = apkHost
	}
	if !strings.EqualFold(u.Host, host) {
		return api.GetCredentialsResponse{}, fmt.Errorf("this helper only supports %s, got %q", host, u.Host)
	}
	credential, err := r.credential(ctx)
	if err != nil {
		return api.GetCredentialsResponse{}, err
	}
	// The credential is the HTTP Basic userinfo "<identity-id>:<token>". For a
	// Chainguard pull token the username is the token's identity id (not the
	// literal "user"), and identity id and token rotate together, so they are
	// stored and looked up as a single value.
	if _, _, ok := strings.Cut(credential, ":"); !ok {
		return api.GetCredentialsResponse{}, fmt.Errorf("%s credential must be in <identity-id>:<token> form", host)
	}
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(credential))
	return api.GetCredentialsResponse{
		Headers: map[string][]string{"Authorization": {auth}},
	}, nil
}
