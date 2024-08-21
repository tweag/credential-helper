package gcs

import (
	"context"
	"errors"
	"net/url"
	"time"

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
	parsedURL, error := url.Parse(req.URI)
	if error != nil {
		return api.GetCredentialsResponse{}, error
	}

	if parsedURL.Scheme != "https" {
		return api.GetCredentialsResponse{}, errors.New("only https is supported")
	}

	if parsedURL.Host != "storage.googleapis.com" {
		return api.GetCredentialsResponse{}, errors.New("only storage.googleapis.com is supported")
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
// For GCS, the same token can be used for all requests.
func (g *GCS) CacheKey(req api.GetCredentialsRequest) string {
	return "https://storage.googleapis.com/"
}
