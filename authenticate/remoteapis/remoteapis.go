package remoteapis

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/tweag/credential-helper/api"
	"github.com/tweag/credential-helper/authenticate/internal/helperconfig"
	"github.com/tweag/credential-helper/authenticate/internal/lookupchain"
	"github.com/tweag/credential-helper/logging"
)

// well-known grpc names (name of the Java package and the name of the service in the .proto file)
const (
	GOOGLE_BYTESTREAM_BYTESTREAM                  = "google.bytestream.ByteStream"
	GOOGLE_DEVTOOLS_BUILD_V1_PUBLISHBUILDEVENT    = "google.devtools.build.v1.PublishBuildEvent"
	REMOTE_ASSET_V1_FETCH                         = "build.bazel.remote.asset.v1.Fetch"
	REMOTE_ASSET_V1_PUSH                          = "build.bazel.remote.asset.v1.Push"
	REMOTE_EXECUTION_V2_ACTIONCACHE               = "build.bazel.remote.execution.v2.ActionCache"
	REMOTE_EXECUTION_V2_CAPABILITIES              = "build.bazel.remote.execution.v2.Capabilities"
	REMOTE_EXECUTION_V2_CONTENTADDRESSABLESTORAGE = "build.bazel.remote.execution.v2.ContentAddressableStorage"
	REMOTE_EXECUTION_V2_EXECUTION                 = "build.bazel.remote.execution.v2.Execution"
)

type RemoteAPIs struct{}

// CacheKey returns a cache key for the given request.
// For remote apis, the full URI is a good cache key.
func (g *RemoteAPIs) CacheKey(req api.GetCredentialsRequest) string {
	return req.URI
}

func (g *RemoteAPIs) SetupInstructionsForURI(ctx context.Context, uri string) string {
	parsedURL, error := url.Parse(uri)
	if error != nil {
		parsedURL = &url.URL{}
	}

	var lookupChainInstructions string
	cfg, err := configFromContext(ctx, parsedURL)
	if err == nil {
		chain := lookupchain.New(cfg.LookupChain)
		lookupChainInstructions = chain.SetupInstructions("default", "secret sent to remote APIs as an authentication token or basic auth credentials")
	} else {
		lookupChainInstructions = fmt.Sprintf("due to a configuration parsing issue, no further setup instructions are available: %v", err)
	}

	var rbeSystemInstructions string
	switch {
	case strings.HasPrefix(uri, "https://remote.buildbuddy.io/"):
		rbeSystemInstructions = `For BuildBuddy, visit https://app.buildbuddy.io/docs/setup/ and copy the secret after "x-buildbuddy-api-key=". Use the header_name "x-buildbuddy-api-key" in the configuration.`
	default:
		rbeSystemInstructions = "Cannot infer RBE provider based on uri. Skipping provider-specific setup instructions."
	}

	return fmt.Sprintf("%s refers to a remote build execution (RBE) system (a gRPC endpoint used for remote execution, remote caching, or related purposes).\n%s\n\n%s", uri, rbeSystemInstructions, lookupChainInstructions)
}

func (RemoteAPIs) Resolver(ctx context.Context) (api.Resolver, error) {
	return &RemoteAPIs{}, nil
}

// Get implements the get command of the credential-helper spec:
//
// https://github.com/EngFlow/credential-helper-spec/blob/main/spec.md#get
func (g *RemoteAPIs) Get(ctx context.Context, req api.GetCredentialsRequest) (api.GetCredentialsResponse, error) {
	parsedURL, error := url.Parse(req.URI)
	if error != nil {
		return api.GetCredentialsResponse{}, error
	}

	// the scheme for remote APIs appears to be https for all of the following:
	// - https://
	// - grpc://
	// - grpcs://
	//
	// only unencrypted http:// (using the HTTP/1.1 cache protocl) uses a different scheme
	// for simplicity, we only support the grpc(s) remote APIs here
	if parsedURL.Scheme != "https" && parsedURL.Scheme != "grpc" && parsedURL.Scheme != "grpcs" {
		if parsedURL.Scheme == "grpc" || parsedURL.Scheme == "grpcs" {
			logging.Errorf("expecting to see https scheme (which is the URI that Bazel norrmally forwards to the credential helper for remoteapis), but got %q", parsedURL.Scheme)
		} else {
			return api.GetCredentialsResponse{}, fmt.Errorf("only https, grpc, and grpcs are supported, but got %q", parsedURL.Scheme)
		}
	}

	// the following only works for grpc (and not HTTP/1.1)
	rpcName, hasPrefix := strings.CutPrefix(parsedURL.Path, "/")
	if !hasPrefix {
		return api.GetCredentialsResponse{}, errors.New("remote execution API path must start with /")
	}
	switch rpcName {
	default:
		return api.GetCredentialsResponse{}, fmt.Errorf("unknown remote execution API path %q - maybe you are trying to use HTTP/1.1, but currently only gRPC is supported", parsedURL.Path)
	case GOOGLE_BYTESTREAM_BYTESTREAM:
	case GOOGLE_DEVTOOLS_BUILD_V1_PUBLISHBUILDEVENT:
	case REMOTE_ASSET_V1_FETCH:
	case REMOTE_ASSET_V1_PUSH:
	case REMOTE_EXECUTION_V2_ACTIONCACHE:
	case REMOTE_EXECUTION_V2_CAPABILITIES:
	case REMOTE_EXECUTION_V2_CONTENTADDRESSABLESTORAGE:
	case REMOTE_EXECUTION_V2_EXECUTION:
	}

	cfg, err := configFromContext(ctx, parsedURL)
	if err != nil {
		return api.GetCredentialsResponse{}, fmt.Errorf("getting configuration fragment for remotapis helper and url %s: %w", req.URI, err)
	}

	chain := lookupchain.New(cfg.LookupChain)
	secret, err := chain.Lookup("default")
	if err != nil {
		return api.GetCredentialsResponse{}, err
	}

	headerName := cfg.HeaderName
	secretEncoding := func(secret string) string {
		// by default, the secret is directly used as a header value
		return secret
	}
	switch cfg.AuthMethod {
	case "header":
		if len(cfg.HeaderName) == 0 {
			return api.GetCredentialsResponse{}, errors.New(`header_name must be set for auth method "header"`)
		}
	case "basic_auth":
		// bazel-remote only supports basic auth.
		// It tries to read the standard grpc metadata key ":authority" to get the username and password.
		// This is special header that the credential helper cannot provide.
		// As a fallback for proxies, bazel-remote also reads the grpc metadata key "authorization" to get the username and password encoded as a base64 string.
		if len(cfg.HeaderName) > 0 {
			headerName = cfg.HeaderName
		} else {
			headerName = "authorization"
		}
		secretEncoding = func(secret string) string {
			return "Basic " + base64.StdEncoding.EncodeToString([]byte(secret))
		}
	default:
		return api.GetCredentialsResponse{}, fmt.Errorf(`unknown auth method %q. Possible values are "header" and "basic_auth"`, cfg.AuthMethod)
	}

	return api.GetCredentialsResponse{
		Headers: map[string][]string{
			headerName: {secretEncoding(secret)},
		},
	}, nil
}

type configFragment struct {
	// AuthMethod is the method used to authenticate with the remote API.
	// Valid values are:
	//   - "header", which works BuildBuddy and other services that use a HTTP header directly. The secret is used as the value of the header.
	//   - "basic_auth", which works for bazel-remote. The secret should be of the form "username:password".
	// It defaults to "header".
	AuthMethod string `json:"auth_method"`
	// HeaderName is the name of the header to set the secret in.
	HeaderName string `json:"header_name"`
	// LookupChain defines the order in which secrets are looked up from sources.
	// Each element is a string that identifies a secret source.
	// It defaults to the sources "env", "keyring".
	LookupChain lookupchain.Config `json:"lookup_chain"`
}

func configFromContext(ctx context.Context, uri *url.URL) (configFragment, error) {
	if cfg, ok := wellKnownServices[uri.Host]; ok {
		return cfg, nil
	}

	return helperconfig.FromContext(ctx, configFragment{
		AuthMethod: "header",
		LookupChain: lookupchain.Default([]lookupchain.Source{
			&lookupchain.Env{
				Source:  "env",
				Name:    "CREDENTIAL_HELPER_REMOTEAPIS_SECRET",
				Binding: "default",
			},
			&lookupchain.Keyring{
				Source:  "keyring",
				Service: "tweag-credential-helper:remoteapis",
				Binding: "default",
			},
		}),
	})
}

var wellKnownServices = map[string]configFragment{
	"remote.buildbuddy.io": {
		AuthMethod: "header",
		HeaderName: "x-buildbuddy-api-key",
		LookupChain: lookupchain.Default([]lookupchain.Source{
			&lookupchain.Env{
				Source:  "env",
				Name:    "BUILDBUDDY_API_KEY",
				Binding: "default",
			},
			&lookupchain.Keyring{
				Source:  "keyring",
				Service: "tweag-credential-helper:buildbuddy_api_key",
				Binding: "default",
			},
		}),
	},
}
