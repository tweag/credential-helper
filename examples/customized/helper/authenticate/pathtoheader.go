package authenticate

import (
	"context"
	"net/url"
	"time"

	"github.com/tweag/credential-helper/api"
)

// PathToHeader is a credential helper that
// takes a request path and returns
// a custom header including it.
type PathToHeader struct{}

// CacheKey returns a cache key for the given request.
// For PathToHeader, the path component of the URI is used as a cache key.
func (PathToHeader) CacheKey(req api.GetCredentialsRequest) string {
	parsedURL, err := url.Parse(req.URI)
	if err != nil {
		return "PathToHeaderUnknown/" + req.URI
	}
	return "PathToHeader/" + parsedURL.Path
}

func (PathToHeader) Resolver(ctx context.Context) (api.Resolver, error) {
	return PathToHeader{}, nil
}

// Get implements the get command of the credential-helper spec:
//
// https://github.com/EngFlow/credential-helper-spec/blob/main/spec.md#get
func (PathToHeader) Get(ctx context.Context, req api.GetCredentialsRequest) (api.GetCredentialsResponse, error) {
	parsedURL, error := url.Parse(req.URI)
	if error != nil {
		return api.GetCredentialsResponse{}, error
	}

	return api.GetCredentialsResponse{
		Expires: time.Now().Add(time.Hour * 24).UTC().Format(time.RFC3339),
		Headers: map[string][]string{
			"X-Tweag-Credential-Helper-Path": {parsedURL.Path},
		},
	}, nil
}
