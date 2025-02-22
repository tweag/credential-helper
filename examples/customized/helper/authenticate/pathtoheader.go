package authenticate

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/tweag/credential-helper/api"
	"github.com/tweag/credential-helper/registry"
)

func init() {
	// This code runs when the program starts.
	// It registers this helper with the registry under the name `pathtoheader`.
	// The registry is used by the agent to look up helpers by name.
	registry.Register("pathtoheader", PathToHeader{})
}

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

	headers := map[string][]string{"X-Tweag-Credential-Helper-Path": {parsedURL.Path}}

	// implement httpbin basic-auth for testing
	// parts: "", "basic-auth", username, password
	bAuthUsernameAndPass := strings.Split(parsedURL.Path, "/")
	if len(bAuthUsernameAndPass) == 4 && bAuthUsernameAndPass[1] == "basic-auth" {
		bAuthCredentialsPlain := fmt.Sprintf("%s:%s", bAuthUsernameAndPass[2], bAuthUsernameAndPass[3])
		headers["Authorization"] = []string{"Basic " + base64.StdEncoding.EncodeToString([]byte(bAuthCredentialsPlain))}
	}

	return api.GetCredentialsResponse{
		Expires: time.Now().Add(time.Hour * 24).UTC().Format(time.RFC3339),
		Headers: headers,
	}, nil
}
