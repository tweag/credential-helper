package null

import (
	"context"

	"github.com/tweag/credential-helper/api"
)

// Null is a credential helper that does not perform any authentication.
type Null struct{}

// Get implements the get command of the credential-helper spec:
//
// https://github.com/EngFlow/credential-helper-spec/blob/main/spec.md#get
func (n Null) Get(_ context.Context, _ api.GetCredentialsRequest) (api.GetCredentialsResponse, error) {
	return api.GetCredentialsResponse{}, nil
}

// CacheKey returns a cache key for the given request.
// For Null, no cache key is returned (do not cache).
func (n Null) CacheKey(_ api.GetCredentialsRequest) string {
	return ""
}
