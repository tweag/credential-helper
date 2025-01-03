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

	cfg, err := o.deriveAuthConfig(registry, wwwAuthenticate.Service, wwwAuthenticate.Realm)
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

func (o *OCIResolver) deriveAuthConfig(registry, service, realm string) (AuthConfig, error) {
	o.mux.Lock()
	defer o.mux.Unlock()

	logging.Debugf("deriving auth config for registry %q", registry)

	if authenticator, ok := o.authenticatorForRegistry[registry]; ok {
		customAuthConfig, err := authenticator(registry, service, realm)
		if err != nil {
			return AuthConfig{}, err
		}
		var empty AuthConfig
		if customAuthConfig != empty {
			return customAuthConfig, nil
		}
		// if the custom authenticator returned an empty config
		// we fall back to the Docker config
	}

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
		logging.Debugf("no auth config found for registry %q - trying anonymous auth", registry)
	}

	return dockerAuthConfig, nil
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
