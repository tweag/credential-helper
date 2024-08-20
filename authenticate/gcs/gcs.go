package gcs

import (
	"context"

	"github.com/tweag/credential-helper/api"
	"golang.org/x/oauth2"
	gauth "golang.org/x/oauth2/google"
)

type GCS struct {
	tokenSource oauth2.TokenSource
}

func New(ctx context.Context) (*GCS, error) {
	credentials, err := gauth.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/devstorage.read_only")
	if err != nil {
		return nil, err
	}
	return &GCS{tokenSource: credentials.TokenSource}, nil
}

// Get implements the get command of the credential-helper spec:
//
// https://github.com/EngFlow/credential-helper-spec/blob/main/spec.md#get
func (g *GCS) Get(ctx context.Context, req api.GetCredentialsRequest) (api.GetCredentialsResponse, error) {
	token, err := g.tokenSource.Token()
	if err != nil {
		return api.GetCredentialsResponse{}, err
	}
	return api.GetCredentialsResponse{
		Headers: map[string][]string{
			"Authorization": {"Bearer " + token.AccessToken},
		},
	}, nil
}

// CacheKey returns a cache key for the given request.
// For GCS, the same token can be used for all requests.
func (g *GCS) CacheKey(req api.GetCredentialsRequest) string {
	return "https://storage.googleapis.com/"
}
