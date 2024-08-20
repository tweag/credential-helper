package cache

import (
	"context"
	"sync"
	"time"

	"github.com/tweag/credential-helper/api"
)

type MemCache struct {
	cache map[string]api.CachableGetCredentialsResponse
	mux   sync.RWMutex
}

func NewMemCache() *MemCache {
	return &MemCache{
		cache: make(map[string]api.CachableGetCredentialsResponse),
	}
}

func (c *MemCache) Retrieve(ctx context.Context, cacheKey string) (api.GetCredentialsResponse, error) {
	c.mux.RLock()
	defer c.mux.RUnlock()

	if cacheValue, ok := c.cache[cacheKey]; ok {
		// TODO: check if the cache value is expired
		// TODO: think about ways to prune the cache
		return cacheValue.Response, nil
	}
	return api.GetCredentialsResponse{}, api.CacheMiss
}

func (c *MemCache) Store(ctx context.Context, cacheValue api.CachableGetCredentialsResponse) error {
	if len(cacheValue.CacheKey) == 0 || len(cacheValue.Response.Expires) == 0 {
		return nil
	}

	c.mux.Lock()
	defer c.mux.Unlock()
	c.cache[cacheValue.CacheKey] = cacheValue
	return nil
}

func (c *MemCache) Prune(_ context.Context) error {
	c.mux.Lock()
	defer c.mux.Unlock()

	for key := range c.cache {
		cacheValue := c.cache[key]

		ts, err := time.Parse(time.RFC3339, cacheValue.Response.Expires)
		if err != nil {
			delete(c.cache, key)
			continue
		}

		if ts.Before(time.Now()) {
			delete(c.cache, key)
		}
	}
	return nil
}
