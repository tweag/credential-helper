package oci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/tweag/credential-helper/api"
	"github.com/tweag/credential-helper/authenticate/internal/helperconfig"
	"github.com/tweag/credential-helper/authenticate/internal/lookupchain"
	"github.com/tweag/credential-helper/logging"
	"golang.org/x/oauth2"
)

func NewFallbackOCI() *OCI {
	return &OCI{
		wwwAuthenticateForRegistry: make(map[string]WWWAuthenticate),
		authenticatorForRegistry:   make(map[string]func(registry, service, realm string) (AuthConfig, error)),
	}
}

func NewCustomOCI(
	wwwAuthenticateForRegistry map[string]WWWAuthenticate,
	authenticatorForRegistry map[string]func(registry, service, realm string) (AuthConfig, error),
	resolveAuthenticators func(ctx context.Context) (map[string]func(registry, service, realm string) (AuthConfig, error), error),
) *OCI {
	return &OCI{
		wwwAuthenticateForRegistry: wwwAuthenticateForRegistry,
		authenticatorForRegistry:   authenticatorForRegistry,
		resolveAuthenicators:       resolveAuthenticators,
	}
}

type OCI struct {
	wwwAuthenticateForRegistry map[string]WWWAuthenticate
	authenticatorForRegistry   map[string]func(registry, service, realm string) (AuthConfig, error)
	resolveAuthenicators       func(ctx context.Context) (map[string]func(registry, service, realm string) (AuthConfig, error), error)
}

func (o *OCI) Resolver(ctx context.Context) (api.Resolver, error) {
	if o.resolveAuthenicators != nil {
		authenticators, err := o.resolveAuthenicators(ctx)
		if err != nil {
			return nil, err
		}
		if o.authenticatorForRegistry == nil {
			o.authenticatorForRegistry = authenticators
		} else {
			for k, v := range authenticators {
				o.authenticatorForRegistry[k] = v
			}
		}
	}

	if o.wwwAuthenticateForRegistry == nil {
		o.wwwAuthenticateForRegistry = make(map[string]WWWAuthenticate)
	}
	if o.authenticatorForRegistry == nil {
		o.authenticatorForRegistry = make(map[string]func(registry, service, realm string) (AuthConfig, error))
	}

	return &OCIResolver{
		wwwAuthenticateForRegistry: o.wwwAuthenticateForRegistry,
		authenticatorForRegistry:   o.authenticatorForRegistry,
	}, nil
}

func (o *OCI) SetupInstructionsForURI(ctx context.Context, uri string) string {
	parsedURL, error := url.Parse(uri)
	if error != nil {
		parsedURL = &url.URL{}
	}

	var lookupChainInstructions []string
	cfg, err := configFromContext(ctx, parsedURL.Host)
	if err == nil {
		chain := lookupchain.New(cfg.LookupChain)
		lookupChainInstructions = append(lookupChainInstructions, chain.SetupInstructions(BindingUsername, "Username"))
		lookupChainInstructions = append(lookupChainInstructions, chain.SetupInstructions(BindingPassword, "Password"))
		lookupChainInstructions = append(lookupChainInstructions, chain.SetupInstructions(BindingAuth, "username:password encoded as base64"))
		lookupChainInstructions = append(lookupChainInstructions, chain.SetupInstructions(BindingIdentityToken, "used for OAuth"))
		lookupChainInstructions = append(lookupChainInstructions, chain.SetupInstructions(BindingRegistryToken, "immediately usable token for the registry - no exchange necessary"))
	} else {
		lookupChainInstructions = []string{fmt.Sprintf("due to a configuration parsing issue, no further setup instructions are available: %v", err)}
	}

	var serviceString string
	switch parsedURL.Host {
	case "ghcr.io":
		serviceString = "GitHub Container Registry image"
	default:
		serviceString = "container image"
	}

	return fmt.Sprintf(`%s is a %s.

The credential helper can be used to download container images from OCI registries.

Default flow:

  If no custom logic exists to obtain tokens for a specific registry,
  the helper parses your docker config (~/.docker/config.json) to obtain credentials for registries.
  This allows you to use any registry that can be used via docker pull, simply by configuring it in advance with docker login

Custom implementations:

  For selected registries, the credential helper implements custom logic for obtaining tokens.

  - GitHub packages / ghcr.io

    For the GitHub container registry, the credential helper uses the same token flow that is also used for the GitHub api.
    You can use a different token for ghcr.io by setting the $GHCR_TOKEN environment variable.

%s`, uri, serviceString, strings.Join(lookupChainInstructions, "\n\n"))
}

// CacheKey returns a cache key for the given request.
// For OCI registries, the registry name, alongside the requested repository, is a good cache key.
func (o *OCI) CacheKey(req api.GetCredentialsRequest) string {
	logging.Debugf("cache key for %q", req.URI)

	registry, repository, ignore, err := deriveRepository(req.URI)
	if err != nil {
		logging.Errorf("deriving oci repository from request: %v", err)
		return ""
	}

	if ignore {
		return "" // this endpoint is not compatible with our authentication scheme
	}

	u := url.URL{
		Scheme: "oci-registry-v2-auth",
		Host:   registry,
		RawQuery: url.Values{
			"repository": {repository},
			// for now we only support pull
			// make this part of the cache key
			// so we can support more actions in the future
			"action": {"pull"},
		}.Encode(),
	}

	return u.String()
}

type OCIResolver struct {
	wwwAuthenticateForRegistry map[string]WWWAuthenticate
	authenticatorForRegistry   map[string]func(registry, service, realm string) (AuthConfig, error)
	mux                        sync.Mutex
}

// Get implements the get command of the credential-helper spec:
//
// https://github.com/EngFlow/credential-helper-spec/blob/main/spec.md#get
func (o *OCIResolver) Get(ctx context.Context, req api.GetCredentialsRequest) (api.GetCredentialsResponse, error) {
	registry, repository, ignore, err := deriveRepository(req.URI)
	if err != nil {
		return api.GetCredentialsResponse{}, err
	}

	if ignore {
		logging.Debugf("ignoring request for %q", req.URI)
		return api.GetCredentialsResponse{}, nil
	}

	scope := "repository:" + repository + ":pull"

	token, err := o.Exchange(ctx, registry, []string{scope})
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
			"Accept": {
				"application/vnd.docker.distribution.manifest.v2+json",
				"application/vnd.oci.image.manifest.v1+json",
				"application/vnd.docker.distribution.manifest.list.v2+json",
				"application/vnd.oci.image.index.v1+json",
			},
			"Docker-Distribution-API-Version": {"registry/2.0"},
		},
	}, nil
}

func (o *OCIResolver) WWWAuthenticate(ctx context.Context, registry string) (WWWAuthenticate, error) {
	o.mux.Lock()
	defer o.mux.Unlock()
	if realm, ok := o.wwwAuthenticateForRegistry[registry]; ok {
		return realm, nil
	}

	u := url.URL{
		Scheme: "https",
		Host:   registry,
		Path:   "/v2/",
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return WWWAuthenticate{}, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return WWWAuthenticate{}, err
	}
	defer resp.Body.Close()

	// TODO: handle 200 OK (no auth required)

	if resp.StatusCode != http.StatusUnauthorized {
		return WWWAuthenticate{}, fmt.Errorf("learning realm via %s: unexpected status code %d", u.String(), resp.StatusCode)
	}

	challenge := resp.Header.Get("WWW-Authenticate")
	if challenge == "" {
		return WWWAuthenticate{}, errors.New("missing WWW-Authenticate header")
	}
	values, err := parseWWWAuthenticate(challenge)
	if err != nil {
		return WWWAuthenticate{}, err
	}
	if customService, ok := values["service"]; ok {
		logging.Debugf("learned custom service %s for registry %s", customService, registry)
	}
	var realm, service string
	var ok bool
	if realm, ok = values["realm"]; !ok {
		return WWWAuthenticate{}, errors.New("missing realm in WWW-Authenticate header")
	}
	if service, ok = values["service"]; !ok {
		return WWWAuthenticate{}, errors.New("missing service in WWW-Authenticate header")
	}
	return WWWAuthenticate{
		Realm:   realm,
		Service: service,
	}, nil
}

func (o *OCIResolver) Exchange(ctx context.Context, registry string, scopes []string) (*oauth2.Token, error) {
	wwwAuthenticate, err := o.WWWAuthenticate(ctx, registry)
	if err != nil {
		return nil, fmt.Errorf("learning realm and service for registry %q: %w", registry, err)
	}

	logging.Debugf("exchange token for registry %v, service %v, realm %v", registry, wwwAuthenticate.Service, wwwAuthenticate.Realm)

	cfg, err := o.deriveAuthConfig(ctx, registry, wwwAuthenticate.Service, wwwAuthenticate.Realm)
	if err != nil {
		return nil, fmt.Errorf("deriving auth config for registry %q: %w", registry, err)
	}

	if cfg.RegistryToken != "" {
		// this token can be used directly
		// without further exchange
		// TODO: check if it's still valid (and refresh if necessary)
		return &oauth2.Token{
			AccessToken: cfg.RegistryToken,
			TokenType:   "bearer",
		}, nil
	}

	// decide which method to use based on the auth config
	if cfg.TokenExchangeMethod == "basic" {
		return basicToken(ctx, wwwAuthenticate.Service, wwwAuthenticate.Realm, scopes, cfg)
	} else if cfg.TokenExchangeMethod == "oauth2" {
		return o.oauthToken(ctx, wwwAuthenticate.Service, wwwAuthenticate.Realm, scopes, cfg)
	} else if cfg.TokenExchangeMethod != "auto" {
		return nil, fmt.Errorf("unsupported token exchange method %q", cfg.TokenExchangeMethod)
	}

	// auto mode
	var token *oauth2.Token
	var tokenErr error
	if cfg.Username == "<token>" || cfg.IdentityToken != "" {
		token, tokenErr = o.oauthToken(ctx, wwwAuthenticate.Service, wwwAuthenticate.Realm, scopes, cfg)
	} else {
		token, tokenErr = basicToken(ctx, wwwAuthenticate.Service, wwwAuthenticate.Realm, scopes, cfg)
	}

	return token, tokenErr
}

// basicToken obtains a token using basic auth.
// See https://distribution.github.io/distribution/spec/auth/token/
func basicToken(ctx context.Context, service, realm string, scopes []string, cfg AuthConfig) (*oauth2.Token, error) {
	logging.Debugf("obtaining token for %q using basic auth", service)

	u, err := url.Parse(realm)
	if err != nil {
		return nil, err
	}

	queryParams := u.Query()
	queryParams.Set("scope", strings.Join(scopes, " "))
	queryParams.Set("service", service)
	u.RawQuery = queryParams.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, err
	}
	if len(cfg.Username) > 0 && len(cfg.Password) > 0 {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	} else {
		logging.Debugf("no username/password found for service %q - trying anonymous auth", service)
		logging.Debugf("basic auth request: %v", u.String())
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		buf := new(strings.Builder)
		if _, err := io.Copy(buf, resp.Body); err == nil {
			logging.Debugf("basic auth response body: %s", buf.String())
		}

		return nil, fmt.Errorf("unexpected status code when obtaining token for %s: %d", service, resp.StatusCode)
	}

	var token BasicAuthToken
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("decoding token response: %w", err)
	}

	return token.Convert(time.Now().UTC()), nil
}

func (o *OCIResolver) oauthToken(ctx context.Context, service, realm string, scopes []string, cfg AuthConfig) (*oauth2.Token, error) {
	values := url.Values{
		"scope":     {strings.Join(scopes, " ")},
		"service":   {service},
		"client_id": {"tweag-credential-helper"},
	}

	if cfg.IdentityToken != "" {
		values.Set("grant_type", "refresh_token")
		values.Set("refresh_token", cfg.IdentityToken)
	} else if cfg.Username != "" && cfg.Password != "" {
		values.Set("grant_type", "password")
		values.Set("username", cfg.Username)
		values.Set("password", cfg.Password)
	} else {
		logging.Debugf("no auth config found for service %q - trying anonymous auth", service)
		return basicToken(ctx, service, realm, scopes, cfg)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, realm, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// oauth token endpoint not found
		// falling back to basic auth
		logging.Debugf("Oauth2 token endpoint %s for service %s returned 404 - falling back to basic auth", realm, service)
		return basicToken(ctx, service, realm, scopes, cfg)
	}

	if resp.StatusCode != http.StatusOK {
		buf := new(strings.Builder)
		if _, err := io.Copy(buf, resp.Body); err == nil {
			logging.Debugf("oauth response body: %s", buf.String())
			logging.Debugf("oauth response status: %s", resp.Status)
			logging.Debugf("oauth response headers: %v", resp.Header)
		}

		return nil, fmt.Errorf("unexpected status code when obtaining token for %s: %d", service, resp.StatusCode)
	}

	var token OAuth2Token
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("decoding token response: %w", err)
	}

	return token.Convert(time.Now().UTC()), nil
}

func (o *OCIResolver) deriveAuthConfig(ctx context.Context, registry, service, realm string) (AuthConfig, error) {
	o.mux.Lock()
	defer o.mux.Unlock()

	logging.Debugf("deriving auth config for registry %q", registry)

	if authenticator, ok := o.authenticatorForRegistry[registry]; ok {
		// try custom authenticator
		customAuthConfig, err := authenticator(registry, service, realm)
		if err != nil {
			return AuthConfig{}, err
		}
		var empty AuthConfig
		if customAuthConfig != empty {
			return customAuthConfig, nil
		}
		// if the custom authenticator returned an empty config
		// we fall back to the lookup_chain and Docker config
	}

	cfg, err := configFromContext(ctx, registry)
	if err != nil {
		return AuthConfig{}, fmt.Errorf("loading config from context: %w", err)
	}

	var authConfig AuthConfig

	if cfg.ParseDockerConfig {
		logging.Debugf("no custom authenticator found for registry %q - trying docker config", registry)

		// try to derive the auth config from the Docker config
		dockerConfig, err := loadDockerConfig()
		if err != nil {
			return AuthConfig{}, fmt.Errorf("loading Docker config: %w", err)
		}

		dockerAuthConfig, found, err := authForHost(dockerConfig, registry)
		if err != nil {
			return AuthConfig{}, fmt.Errorf("finding auth config for registry %q: %w", registry, err)
		}
		if found {
			o.authenticatorForRegistry[registry] = func(registry, service, realm string) (AuthConfig, error) {
				return dockerAuthConfig, nil
			}
		} else {
			logging.Debugf("no auth config found for registry %q", registry)
		}
		authConfig = dockerAuthConfig
	} else {
		logging.Debugf("not parsing Docker config for registry %q", registry)
	}

	authConfig.TokenExchangeMethod = cfg.TokenExchangeMethod

	// try to find overrides for the docker auth config
	// in the lookup chain
	chain := lookupchain.New(cfg.LookupChain)
	unsername, unsernameErr := chain.Lookup(BindingUsername)
	password, passwordErr := chain.Lookup(BindingPassword)
	auth, authErr := chain.Lookup(BindingAuth)
	identityToken, identityTokenErr := chain.Lookup(BindingIdentityToken)
	registryToken, registryTokenErr := chain.Lookup(BindingRegistryToken)

	if unsernameErr == nil {
		logging.Debugf("found username in lookup chain: %s", unsername)
		authConfig.Username = unsername
	}
	if passwordErr == nil {
		logging.Debugf("using password from lookup chain")
		authConfig.Password = password
		if authConfig.Username == "" {
			authConfig.Username = "unset"
		}
	}
	if authErr == nil {
		logging.Debugf("using auth from lookup chain")
		authConfig.Auth = auth
	}
	if identityTokenErr == nil {
		logging.Debugf("using identity token from lookup chain")
		authConfig.IdentityToken = identityToken
	}
	if registryTokenErr == nil {
		logging.Debugf("using registry token from lookup chain")
		authConfig.RegistryToken = registryToken
	}

	return authConfig, nil
}

func deriveRepository(uri string) (registry, repository string, ignore bool, err error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", "", false, err
	}
	if u.Scheme != "https" {
		return "", "", false, fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	if u.Host == "" {
		return "", "", false, errors.New("missing host")
	}
	if !strings.HasPrefix(u.Path, "/v2/") {
		// this is not a request to the OCI registry API
		return "", "", true, nil
	}

	if u.Path == "/v2/" || u.Path == "/v2" {
		// this is a request to the `/v2/` endpoint directly
		// the client probably doesn't want us to authenticate
		// this.
		return u.Host, "", true, nil
	}

	endpoint := strings.TrimPrefix(u.Path, "/v2/")

	// check for well-known pull endpoints (for now we don't support uploads)
	// /v2/<name>/blobs/<digest>
	// /v2/<name>/manifests/<reference>
	// /v2/<name>/tags/list

	if strings.HasSuffix(endpoint, "/tags/list") {
		return u.Host, strings.TrimSuffix(endpoint, "/tags/list"), false, nil
	}

	parts := strings.Split(endpoint, "/")
	if len(parts) < 3 {
		// whatever endpoint this is, it's not one we can authenticate
		return u.Host, "", true, nil
	}

	var suffix string

	if parts[len(parts)-2] == "blobs" {
		suffix = "/blobs/" + parts[len(parts)-1]
	} else if parts[len(parts)-2] == "manifests" {
		suffix = "/manifests/" + parts[len(parts)-1]
	} else {
		// whatever endpoint this is, it's not one we can authenticate
		return u.Host, "", true, nil
	}

	return u.Host, strings.TrimSuffix(endpoint, suffix), false, nil
}

// GuessOCIRegistry returns true if the given URI is likely to be an OCI registry.
// Only guesses if $CREDENTIAL_HELPER_GUESS_OCI_REGISTRY is set to "1".
func GuessOCIRegistry(uri string) bool {
	value, ok := os.LookupEnv(api.GuessOCIRegistryEnv)
	if !ok || value != "1" {
		return false
	}
	_, _, ignore, err := deriveRepository(uri)
	if err != nil {
		return false
	}
	if ignore {
		return false
	}
	return true
}

const (
	BindingUsername      = "username"
	BindingPassword      = "password"
	BindingAuth          = "auth"
	BindingIdentityToken = "identitytoken"
	BindingRegistryToken = "registrytoken"
)

type configFragment struct {
	ParseDockerConfig bool `json:"parse_docker_config,omitempty"`
	// TokenExchangeMethod is the method used to exchange the token.
	// It can be "auto", "oauth2", or "basic".
	// Defaults to "auto".
	TokenExchangeMethod string `json:"token_exchange_method,omitempty"`
	// LookupChain defines the order in which secrets are looked up from sources.
	// Each element is a string that identifies a secret source.
	LookupChain lookupchain.Config `json:"lookup_chain,omitempty"`
}

func configFromContext(ctx context.Context, registry string) (configFragment, error) {
	cfg := configFragment{
		ParseDockerConfig:   true,
		TokenExchangeMethod: "auto",
		LookupChain:         lookupchain.Default(lookupChainByRegistry[registry]),
	}

	return helperconfig.FromContext(ctx, cfg)
}

var lookupChainByRegistry = map[string][]lookupchain.Source{
	"ghcr.io": {
		// username
		&lookupchain.Env{
			Source:  "env",
			Name:    "GITHUB_ACTOR",
			Binding: BindingUsername,
		},
		&lookupchain.Static{
			Source: "env",
			// ghcr.io requires a username to be set, but doesn't validate it
			Value:   "unset",
			Binding: BindingUsername,
		},

		// password
		&lookupchain.Env{
			Source:  "env",
			Name:    "GHCR_TOKEN",
			Binding: BindingPassword,
		},
		&lookupchain.Env{
			Source:  "env",
			Name:    "GH_TOKEN",
			Binding: BindingPassword,
		},
		&lookupchain.Env{
			Source:  "env",
			Name:    "GITHUB_TOKEN",
			Binding: BindingPassword,
		},
		&lookupchain.Keyring{
			Source:  "keyring",
			Service: "gh:github.com",
			Binding: BindingPassword,
		},
	},
}
