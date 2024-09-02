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
	CacheKey string
	Response GetCredentialsResponse
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

// Getter is the interface that must be implemented by credential helpers.
type Getter interface {
	Get(context.Context, GetCredentialsRequest) (GetCredentialsResponse, error)
	CacheKey(GetCredentialsRequest) string
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
	AgentSocketPath     = "CREDENTIAL_HELPER_AGENT_SOCKET_PATH"
	AgentPidPath        = "CREDENTIAL_HELPER_AGENT_PID_PATH"
	LogLevelEnv         = "CREDENTIAL_HELPER_LOGGING"
)
