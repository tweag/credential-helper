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

const host = "apk.cgr.dev"

type configFragment struct {
	LookupChain lookupchain.Config `json:"lookup_chain"`
}

func configFromContext(ctx context.Context) (configFragment, error) {
	return helperconfig.FromContext(ctx, configFragment{
		LookupChain: lookupchain.Default([]lookupchain.Source{
			&lookupchain.Env{
				Source:  lookupchain.SourceEnv,
				Name:    "CGR_APK_CREDENTIAL",
				Binding: "default",
			},
		}),
	})
}

type ChainguardAPK struct{}

func (c *ChainguardAPK) Resolver(ctx context.Context) (api.Resolver, error) {
	cfg, err := configFromContext(ctx)
	if err != nil {
		return nil, err
	}
	chain := lookupchain.New(cfg.LookupChain)
	return &ChainguardAPKResolver{
		credential: func(context.Context) (string, error) { return chain.Lookup("default") },
	}, nil
}

func (c *ChainguardAPK) CacheKey(req api.GetCredentialsRequest) string {
	u, err := url.Parse(req.URI)
	if err != nil {
		return ""
	}
	return u.Host
}

func (c *ChainguardAPK) SetupInstructionsForURI(ctx context.Context, uri string) string {
	cfg, err := configFromContext(ctx)
	if err != nil {
		return fmt.Sprintf("%s: configuration error: %v", uri, err)
	}
	chain := lookupchain.New(cfg.LookupChain)
	return fmt.Sprintf(`%s is a Chainguard apk repository.

The credential helper authenticates apk package downloads using a Chainguard pull
token, sent as HTTP Basic auth. Because each "chainctl auth pull-token" invocation
mints a new identity, the pull token's identity id and the token itself rotate
together, so the credential is stored as a single "<identity-id>:<token>" string
(the HTTP Basic user:password userinfo). Mint it with:

    chainctl auth pull-token --repository=apk --output=env --parent=<ORG>
    # then store "${CHAINGUARD_IDENTITY_ID}:${CHAINGUARD_TOKEN}"

%s`, uri, chain.SetupInstructions("default", "Chainguard apk <identity-id>:<token> sent as HTTP Basic auth"))
}

type ChainguardAPKResolver struct {
	credential func(context.Context) (string, error)
}

func (r *ChainguardAPKResolver) Get(ctx context.Context, req api.GetCredentialsRequest) (api.GetCredentialsResponse, error) {
	u, err := url.Parse(req.URI)
	if err != nil {
		return api.GetCredentialsResponse{}, err
	}
	if !strings.EqualFold(u.Host, host) {
		return api.GetCredentialsResponse{}, fmt.Errorf("chainguard-apk only supports %s, got %q", host, u.Host)
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
		return api.GetCredentialsResponse{}, fmt.Errorf("chainguard-apk credential must be in <identity-id>:<token> form")
	}
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(credential))
	return api.GetCredentialsResponse{
		Headers: map[string][]string{"Authorization": {auth}},
	}, nil
}
