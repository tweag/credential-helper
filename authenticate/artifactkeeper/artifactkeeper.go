// Package artifactkeeper authenticates downloads from an Artifact Keeper
// (https://github.com/artifact-keeper/artifact-keeper) instance.
package artifactkeeper

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

type configFragment struct {
	LookupChain lookupchain.Config `json:"lookup_chain"`
}

func configFromContext(ctx context.Context) (configFragment, error) {
	return helperconfig.FromContext(ctx, configFragment{
		LookupChain: lookupchain.Default([]lookupchain.Source{
			&lookupchain.Env{
				Source:  lookupchain.SourceEnv,
				Name:    "ARTIFACT_KEEPER_CREDENTIAL",
				Binding: "default",
			},
		}),
	})
}

// ArtifactKeeper sends a "<user>:<token>" credential (a service account name
// and one of its API tokens, or a user and password) as HTTP Basic auth.
//
// Artifact Keeper is self-hosted, so there is no fixed host: the helper serves
// whatever host .tweag-credential-helper.json and --credential_helper route to
// it. It is usually a pull-through cache listed before the origin URL (for
// example with Bazel's --downloader_config), so a missing credential is not an
// error: the helper sends no Authorization header, Artifact Keeper answers 401
// and Bazel moves on to the next URL.
type ArtifactKeeper struct{}

func (a *ArtifactKeeper) Resolver(ctx context.Context) (api.Resolver, error) {
	cfg, err := configFromContext(ctx)
	if err != nil {
		return nil, err
	}
	chain := lookupchain.New(cfg.LookupChain)
	return &Resolver{
		credential: func(context.Context) (string, error) { return chain.Lookup("default") },
	}, nil
}

// CacheKey returns the host: one Artifact Keeper instance takes one credential
// for all of its repositories.
func (a *ArtifactKeeper) CacheKey(req api.GetCredentialsRequest) string {
	u, err := url.Parse(req.URI)
	if err != nil {
		return ""
	}
	return u.Host
}

func (a *ArtifactKeeper) SetupInstructionsForURI(ctx context.Context, uri string) string {
	cfg, err := configFromContext(ctx)
	if err != nil {
		return fmt.Sprintf("%s: configuration error: %v", uri, err)
	}
	chain := lookupchain.New(cfg.LookupChain)
	return fmt.Sprintf(`%s is served by Artifact Keeper.

The credential helper sends a "<user>:<token>" credential as HTTP Basic auth.
For read-only clients, create a service account in Artifact Keeper with
read scopes, mint an API token for it, and store "<service-account>:<token>".
Without a credential the helper sends no Authorization header, so the request
is anonymous.

%s`, uri, chain.SetupInstructions("default", "Artifact Keeper <user>:<token> sent as HTTP Basic auth"))
}

type Resolver struct {
	credential func(context.Context) (string, error)
}

func (r *Resolver) Get(ctx context.Context, req api.GetCredentialsRequest) (api.GetCredentialsResponse, error) {
	if _, err := url.Parse(req.URI); err != nil {
		return api.GetCredentialsResponse{}, err
	}
	credential, err := r.credential(ctx)
	if lookupchain.IsNotFoundErr(err) || (err == nil && credential == "") {
		return api.GetCredentialsResponse{}, nil
	}
	if err != nil {
		return api.GetCredentialsResponse{}, err
	}
	if _, _, ok := strings.Cut(credential, ":"); !ok {
		return api.GetCredentialsResponse{}, fmt.Errorf("artifact-keeper credential must be in <user>:<token> form")
	}
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(credential))
	return api.GetCredentialsResponse{
		Headers: map[string][]string{"Authorization": {auth}},
	}, nil
}
