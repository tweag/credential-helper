package api

import (
	"context"
	"encoding/json"
	"errors"
)

// GetCredentialsRequest is defined in the credential-helper spec:
// https://github.com/EngFlow/credential-helper-spec/blob/main/schemas/get-credentials-request.schema.json
type GetCredentialsRequest struct {
	URI string `json:"uri"`
}

// GetCredentialsResponse is defined in the credential-helper spec:
// https://github.com/EngFlow/credential-helper-spec/blob/main/schemas/get-credentials-response.schema.json
type GetCredentialsResponse struct {
	Expires string              `json:"expires,omitempty"`
	Headers map[string][]string `json:"headers,omitempty"`
}

// CachableGetCredentialsResponse is a GetCredentialsResponse with an additional cache key.
// A response with a non-empy cache key and a non-empty Expires field may be cached.
type CachableGetCredentialsResponse struct {
	CacheKey string                 `json:"cacheKey,omitempty"`
	Response GetCredentialsResponse `json:"response,omitempty"`
}

var (
	AgentRequestRetrieve = "retrieve"
	AgentRequestStore    = "store"
	AgentRequestPrune    = "prune"
	AgentRequestShutdown = "shutdown"
)

var (
	AgentResponseOK        = "ok"
	AgentResponseCacheMiss = "cache-miss"
	AgentResponseError     = "error"
)

type AgentRequest struct {
	Method  string          `json:"method"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type AgentResponse struct {
	Status  string          `json:"status"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Resolver is used to retrieve credentials for a given URI.
type Resolver interface {
	Get(context.Context, GetCredentialsRequest) (GetCredentialsResponse, error)
}

// CacheKeyer is used to generate a cache key for a given request.
type CacheKeyer interface {
	CacheKey(GetCredentialsRequest) string
}

// Helper is the interface that must be implemented by credential helpers
type Helper interface {
	Resolver(context.Context) (Resolver, error)
	CacheKeyer
}

// Cache is the interface that must be implemented by cache implementations.
type Cache interface {
	Retrieve(context.Context, string) (GetCredentialsResponse, error)
	Store(context.Context, CachableGetCredentialsResponse) error
	Prune(context.Context) error
}

var CacheMiss = errors.New("cache miss")

// Environment variable names used by the credential helper.
const (
	Standalone          = "CREDENTIAL_HELPER_STANDALONE"
	CredentialHelperBin = "CREDENTIAL_HELPER_BIN"
	AgentSocketPath     = "CREDENTIAL_HELPER_AGENT_SOCKET"
	AgentPidPath        = "CREDENTIAL_HELPER_AGENT_PID"
	LogLevelEnv         = "CREDENTIAL_HELPER_LOGGING"
)

// HelperFactory chooses a credential helper (like s3, gcs, github, ...) based on the raw uri.
type HelperFactory func(string) (Helper, error)

type NewCache func() Cache
