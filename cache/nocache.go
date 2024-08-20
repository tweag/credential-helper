package cache

import (
	"context"

	"github.com/tweag/credential-helper/api"
)

// NoCache is a cache that does not cache anything.
type NoCache struct{}

func (f *NoCache) Retrieve(context.Context, string) (api.GetCredentialsResponse, error) {
	return api.GetCredentialsResponse{}, api.CacheMiss
}

func (f *NoCache) Store(context.Context, api.CachableGetCredentialsResponse) error {
	return nil
}

func (f *NoCache) Prune(context.Context) error {
	return nil
}
