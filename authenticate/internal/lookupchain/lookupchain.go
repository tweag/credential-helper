package lookupchain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"time"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	secretmanagerpb "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	keyring "github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
	gauth "golang.org/x/oauth2/google"
	idtoken "google.golang.org/api/idtoken"
	option "google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	SourceEnv              = "env"
	SourceKeyring          = "keyring"
	SourceStatic           = "static"
	SourceGoogle           = "google"
	SourceGCPSecretManager = "gcp_secretmanager"
)

type LookupChain struct {
	config Config
}

func New(config Config) *LookupChain {
	return &LookupChain{config: config}
}

// Lookup looks up a binding in the chain.
// It returns the first value found, or an error.
func (c *LookupChain) Lookup(binding string) (string, error) {
	if len(c.config) == 0 {
		return "", fmt.Errorf("no sources configured to look up binding %q", binding)
	}
	var errs []error
	var errSources []string
	for _, entry := range c.config {
		source, err := c.sourceFor(entry)
		if err != nil {
			return "", fmt.Errorf("looking up binding %q: %w", binding, err)
		}
		result, err := source.Lookup(binding)
		if err == nil {
			return result, nil
		}
		if IsNotFoundErr(err) {
			continue
		}
		errs = append(errs, fmt.Errorf("source %q: %+v", entry.Source, err))
		errSources = append(errSources, entry.Source)
	}
	sourceNames := make([]string, len(c.config))
	for i, entry := range c.config {
		sourceNames[i] = entry.Source
	}

	if len(errs) > 0 {
		return "", fmt.Errorf("no value found for binding %q after errors fetching from sources %v: %+v", binding, errSources, errors.Join(errs...))
	}

	return "", &NotFoundErr{reason: fmt.Sprintf("no value found for binding %q after querying %v", binding, strings.Join(sourceNames, ", "))}
}

func (c *LookupChain) SetupInstructions(binding, meaning string) string {
	instructions := []string{fmt.Sprintf("Instructions for setting up the secret with binding name %q (%s):", binding, meaning)}
	for i, entry := range c.config {
		source, err := c.sourceFor(entry)
		if err != nil {
			instructions = append(instructions, fmt.Sprintf("failed to lookup instuctions for entry %d: %v", i, err))
			continue
		}
		instruction, ok := source.SetupInstructions(binding)
		if ok {
			instructions = append(instructions, instruction)
		}
	}
	if len(instructions) == 1 {
		return fmt.Sprintf("No sources are available for setting up the secret with binding name %q (%s). Refer to the documentation related to configuration files for more information.", binding, meaning)
	}
	return strings.Join(instructions, "\n")
}

func (c *LookupChain) sourceFor(entry ConfigEntry) (Source, error) {
	decoder := json.NewDecoder(bytes.NewReader(entry.RawMessage))
	decoder.DisallowUnknownFields()
	var source Source

	switch entry.Source {
	case SourceEnv:
		var env Env
		if err := decoder.Decode(&env); err != nil {
			return nil, fmt.Errorf("unmarshalling env source: %w", err)
		}
		source = &env
	case SourceKeyring:
		var keyring Keyring
		if err := decoder.Decode(&keyring); err != nil {
			return nil, fmt.Errorf("unmarshalling keyring source: %w", err)
		}
		source = &keyring
	case SourceStatic:
		var static Static
		if err := decoder.Decode(&static); err != nil {
			return nil, fmt.Errorf("unmarshalling static source: %w", err)
		}
		source = &static
	case SourceGoogle:
		var google Google
		if err := decoder.Decode(&google); err != nil {
			return nil, fmt.Errorf("unmarshalling google source: %w", err)
		}
		source = &google
	case SourceGCPSecretManager:
		var gsm GCPSecretManager
		if err := decoder.Decode(&gsm); err != nil {
			return nil, fmt.Errorf("unmarshalling gcp_secretmanager source: %w", err)
		}
		source = &gsm
	default:
		return nil, fmt.Errorf("unknown source %q", entry.Source)
	}

	source.Canonicalize()
	return source, nil
}

type Config []ConfigEntry

// ConfigEntry is a single entry in the lookup chain.
// This form is used when unmarshalling the config.
type ConfigEntry struct {
	// Source is the name of the source used to look up the secret.
	Source string `json:"source"`
	json.RawMessage
}

func (c *ConfigEntry) UnmarshalJSON(data []byte) error {
	// Use special type to learn the source field.
	// This is necessary because the embedded json.RawMessage
	// is greedy and will consume the entire input.
	type SourceConfigEntry struct {
		Source string `json:"source"`
	}
	var entry SourceConfigEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return err
	}
	if entry.Source == "" {
		return errors.New("source must be set")
	}
	c.Source = entry.Source
	c.RawMessage = data
	return nil
}

type Source interface {
	Lookup(binding string) (string, error)
	Canonicalize()
	SetupInstructions(binding string) (string, bool)
}

type Env struct {
	// Source is the name of the source used to look up the secret.
	// It must be "env".
	Source string `json:"source"`
	// Name is the name of the environment variable to look up.
	Name string `json:"name"`
	// Binding binds the value of the environment variable to a well-known name in the helper.
	// If not specified, the value is bound to the default secret of the helper.
	Binding string `json:"binding,omitempty"`
}

func (e *Env) Lookup(binding string) (string, error) {
	if e.Binding != binding {
		return "", &NotFoundErr{}
	}
	val, ok := os.LookupEnv(e.Name)
	if !ok {
		return "", &NotFoundErr{}
	}
	return val, nil
}

func (e *Env) Canonicalize() {
	e.Source = "env"
	if e.Binding == "" {
		e.Binding = "default"
	}
}

func (e *Env) SetupInstructions(binding string) (string, bool) {
	if e.Binding != binding {
		return "", false
	}
	_, success := os.LookupEnv(e.Name)
	var status string
	if success {
		status = "SET"
	} else {
		status = "NOT SET"
	}

	return fmt.Sprintf(" - Export the environment variable %s (status: %s)", e.Name, status), true
}

type Keyring struct {
	// Source is the name of the source used to look up the secret.
	// It must be "keyring".
	Source string `json:"source"`
	// Service is the name of the key to look up in the keyring.
	Service string `json:"service"`
	// Binding binds the value of the keyring secret to a well-known name in the helper.
	// If not specified, the value is bound to the default secret of the helper.
	Binding string `json:"binding,omitempty"`
}

func (k *Keyring) Lookup(binding string) (string, error) {
	if k.Binding != binding {
		return "", &NotFoundErr{}
	}
	val, err := keyring.Get(k.Service, "")
	if errors.Is(err, keyring.ErrNotFound) {
		return "", &NotFoundErr{reason: err.Error()}
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

func (k *Keyring) Canonicalize() {
	k.Source = "keyring"
	if k.Binding == "" {
		k.Binding = "default"
	}
}

func (e *Keyring) SetupInstructions(binding string) (string, bool) {
	if e.Binding != binding {
		return "", false
	}
	_, getErr := keyring.Get(e.Service, "")
	var status string
	if errors.Is(getErr, keyring.ErrNotFound) {
		status = "NOT SET"
	} else if getErr != nil {
		status = "ERROR ACCESSING KEYCHAIN"
	} else {
		status = "SET"
	}

	return fmt.Sprintf(` - Add the secret to the system keyring under the %s service name (status: %s):
    $ %s setup-keyring -f secret.txt %s`, e.Service, status, os.Args[0], e.Service), true
}

type Static struct {
	// Source is the name of the source used to look up the secret.
	// It must be "static".
	Source string `json:"source"`
	// Value is the static value to return.
	Value string `json:"name"`
	// Binding binds the value of the environment variable to a well-known name in the helper.
	// If not specified, the value is bound to the default secret of the helper.
	Binding string `json:"binding,omitempty"`
}

func (s *Static) Lookup(binding string) (string, error) {
	if s.Binding != binding {
		return "", &NotFoundErr{}
	}
	return s.Value, nil
}

func (s *Static) Canonicalize() {
	s.Source = "static"
	if s.Binding == "" {
		s.Binding = "default"
	}
}

func (s *Static) SetupInstructions(binding string) (string, bool) {
	if s.Binding != binding {
		return "", false
	}

	return fmt.Sprintf(" - If none of previous sources work, fall back to the static value %q (status: SET)", s.Value), true
}

type GoogleTokenResponse struct {
	IdentityToken string `json:"id_token,omitempty"`
	oauth2.Token
}

type Google struct {
	// Source must be "google".
	Source string `json:"source"`

	// TokenType selects which kind of token to mint: "access" (default) or "id" / "jwt".
	TokenType string `json:"token_type,omitempty"`

	// Scopes are defined in
	// https://developers.google.com/identity/protocols/oauth2/scopes
	Scopes []string `json:"scopes,omitempty"`

	// Audience is the OIDC target audience.
	// It is used when minting an ID token.
	Audience string `json:"audience,omitempty"`

	// Binding binds the value to a well-known name in the helper.
	// If not specified, the value is bound to the default secret of the helper.
	Binding string `json:"binding,omitempty"`
}

func (g *Google) Lookup(binding string) (string, error) {
	if g.Binding != binding {
		return "", &NotFoundErr{}
	}

	ctx := context.Background()

	switch strings.ToLower(strings.TrimSpace(g.TokenType)) {
	case "", "access":
		creds, err := gauth.FindDefaultCredentials(ctx, g.Scopes...)
		if err != nil {
			return "", fmt.Errorf("failed to find default credentials: %w", err)
		}
		token, err := creds.TokenSource.Token()
		if err != nil {
			return "", fmt.Errorf("failed to get access token: %w", err)
		}
		return "Bearer " + token.AccessToken, nil

	case "id", "jwt":
		creds, ferr := gauth.FindDefaultCredentials(ctx)
		if ferr != nil {
			return "", fmt.Errorf("failed to find default credentials for id token: %w", ferr)
		}

		// If audience is provided, use the standard idtoken approach
		if g.Audience != "" {
			ts, err := idtoken.NewTokenSource(ctx, g.Audience, option.WithCredentials(creds))
			if err != nil {
				return "", fmt.Errorf("failed to create id token source: %w", err)
			}
			tok, err := ts.Token()
			if err != nil {
				return "", fmt.Errorf("failed to mint id token: %w", err)
			}
			// tok.AccessToken holds the JWT (ID token).
			return "Bearer " + tok.AccessToken, nil
		}

		// Alternative flow: use data from application_default_credentials.json
		// to mint an ID token.
		// This is not documented officially, but sometimes this is the only option available.
		if creds.JSON == nil {
			return "", fmt.Errorf("no JSON credentials available for JWT config")
		}

		var requestBody map[string]any
		if err := json.Unmarshal(creds.JSON, &requestBody); err != nil {
			return "", fmt.Errorf("failed to unmarshal JSON credentials: %w", err)
		}
		requestBody["grant_type"] = "refresh_token"

		// Send the request to the Google OAuth2 token endpoint
		jsonBody, err := json.Marshal(requestBody)
		if err != nil {
			return "", fmt.Errorf("failed to marshal request body: %w", err)
		}

		req, err := http.NewRequest("POST", "https://oauth2.googleapis.com/token", bytes.NewBuffer(jsonBody))
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("failed to send request to token endpoint: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return "", fmt.Errorf("token endpoint returned status %d: %s", resp.StatusCode, string(body))
		}

		var tokenResponse GoogleTokenResponse

		if err := json.NewDecoder(resp.Body).Decode(&tokenResponse); err != nil {
			return "", fmt.Errorf("failed to decode token response: %w", err)
		}

		if tokenResponse.IdentityToken == "" {
			return "", fmt.Errorf("token response does not contain an identity token")
		}

		return "Bearer " + tokenResponse.IdentityToken, nil

	default:
		return "", fmt.Errorf(`google: unknown token_type %q (want "access" or "id")`, g.TokenType)
	}
}

func (g *Google) Canonicalize() {
	g.Source = "google"
	if g.Binding == "" {
		g.Binding = "default"
	}
}

func (g *Google) SetupInstructions(binding string) (string, bool) {
	if g.Binding != binding {
		return "", false
	}

	instructions := `
 - Option 1: Using gcloud CLI as a regular user (Recommended)
   1. Install the Google Cloud SDK: https://cloud.google.com/sdk/docs/install
   2. Run:
      $ gcloud auth application-default login
   3. Follow the browser prompts to authenticate

 - Option 2: Using a Service Account Key, OpenID Connect or other authentication mechanisms
   1. Follow Google's documentation: https://cloud.google.com/docs/authentication
   2. Ensure your method sets the Application Default Credentials (ADC)
			environment variable (GOOGLE_APPLICATION_CREDENTIALS) or the credentials file
			is in a well-known location.`
	return instructions, true
}

// Default constructs a partially marshalled Config from a slice of specific config entries.
func Default(in []Source) Config {
	out := make(Config, len(in))
	for i, entry := range in {
		// TODO: fix
		canonicalizeMethod := reflect.ValueOf(entry).MethodByName("Canonicalize")

		if !canonicalizeMethod.IsValid() {
			panic(fmt.Sprintf("constructing default config: invalid value at index %d is missing Canonicalize method", i))
		}
		canonicalizeMethod.Call(nil)

		sourceField := reflect.ValueOf(entry).Elem().FieldByName("Source")
		if !sourceField.IsValid() || sourceField.Type().Kind() != reflect.String {
			panic(fmt.Sprintf("constructing default config: invalid value at index %d is missing Source field", i))
		}

		raw, err := json.Marshal(entry)
		if err != nil {
			panic(fmt.Sprintf("constructing default config: invalid value at index %d when marshaling inner config: %v", i, err))
		}
		out[i] = ConfigEntry{
			Source:     sourceField.String(),
			RawMessage: raw,
		}
	}
	return out
}

type NotFoundErr struct {
	reason string
}

func (e *NotFoundErr) Error() string {
	if e == nil || e.reason == "" {
		return "not found"
	}
	return e.reason
}

func IsNotFoundErr(err error) bool {
	var nfe *NotFoundErr
	return errors.As(err, &nfe)
}

type GCPSecretManager struct {
	// Source must be "gcp_secretmanager".
	Source string `json:"source"`
	// Secret is the fully-qualified secret version resource name, e.g.
	// "projects/<project>/secrets/<name>/versions/latest".
	Secret string `json:"secret"`
	// Binding binds the value to a well-known name in the helper.
	Binding string `json:"binding,omitempty"`

	// accessor is injectable for testing. When nil, the real Secret Manager
	// client is used with Application Default Credentials.
	accessor func(ctx context.Context, name string) (string, error)
}

func (g *GCPSecretManager) Lookup(binding string) (string, error) {
	if g.Binding != binding {
		return "", &NotFoundErr{}
	}
	if g.Secret == "" {
		return "", fmt.Errorf("gcp_secretmanager: secret must be set")
	}
	access := g.accessor
	if access == nil {
		access = accessGCPSecret
	}
	val, err := access(context.Background(), g.Secret)
	if err != nil {
		return "", fmt.Errorf("gcp_secretmanager: accessing %q: %w", g.Secret, err)
	}
	return val, nil
}

func (g *GCPSecretManager) Canonicalize() {
	g.Source = SourceGCPSecretManager
	if g.Binding == "" {
		g.Binding = "default"
	}
}

func (g *GCPSecretManager) SetupInstructions(binding string) (string, bool) {
	if g.Binding != binding {
		return "", false
	}
	return fmt.Sprintf(" - Ensure the secret %q is readable by your Google identity (run `gcloud auth login`, or provide Application Default Credentials such as a service account with roles/secretmanager.secretAccessor)", g.Secret), true
}

const cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

func accessGCPSecret(ctx context.Context, name string) (string, error) {
	_, adcErr := gauth.FindDefaultCredentials(ctx, cloudPlatformScope)
	return accessGCPSecretWithFallback(ctx, name, adcErr, &gcloudCLITokenSource{}, accessGCPSecretAs)
}

// accessGCPSecretWithFallback accesses the secret with Application Default
// Credentials when they are available, and falls back to the given token
// source (backed by `gcloud auth login`) when ADC is unavailable — or when
// ADC resolves to an identity that is denied access to the secret, which is
// the norm on GCE machines where the metadata server always supplies the
// VM's service account.
func accessGCPSecretWithFallback(ctx context.Context, name string, adcErr error, fallback oauth2.TokenSource, access func(context.Context, string, gcpSecretAuth) (string, error)) (string, error) {
	auth, err := chooseGCPSecretAuth(ctx, name, adcErr, fallback)
	if err != nil {
		return "", err
	}
	payload, err := access(ctx, name, auth)
	if err == nil || !auth.useADC || status.Code(err) != codes.PermissionDenied {
		return payload, err
	}
	auth, retryErr := chooseGCPSecretAuth(ctx, name, err, fallback)
	if retryErr != nil {
		return "", retryErr
	}
	payload, retryErr = access(ctx, name, auth)
	if retryErr != nil {
		return "", fmt.Errorf("with gcloud CLI credentials after ADC identity was denied (%v): %w", err, retryErr)
	}
	return payload, nil
}

func accessGCPSecretAs(ctx context.Context, name string, auth gcpSecretAuth) (string, error) {
	var opts []option.ClientOption
	if !auth.useADC {
		opts = append(opts, option.WithTokenSource(auth.tokenSource))
		// A bare user access token carries no quota project, which Secret
		// Manager requires; bill the project that owns the secret.
		if auth.quotaProject != "" {
			opts = append(opts, option.WithQuotaProject(auth.quotaProject))
		}
	}
	client, err := secretmanager.NewClient(ctx, opts...)
	if err != nil {
		return "", err
	}
	defer client.Close()
	resp, err := client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{Name: name})
	if err != nil {
		return "", err
	}
	return string(resp.GetPayload().GetData()), nil
}

// gcpSecretAuth describes how the Secret Manager client authenticates: via
// Application Default Credentials, or via an explicit token source and quota
// project.
type gcpSecretAuth struct {
	useADC       bool
	tokenSource  oauth2.TokenSource
	quotaProject string
}

// chooseGCPSecretAuth prefers Application Default Credentials (adcErr is nil
// when they are usable) and falls back to the given token source, which
// draws on the user credentials established by `gcloud auth login`. The
// fallback token is minted eagerly so that a missing gcloud login surfaces
// here as an actionable error instead of a failed API call later.
func chooseGCPSecretAuth(_ context.Context, secretName string, adcErr error, fallback oauth2.TokenSource) (gcpSecretAuth, error) {
	if adcErr == nil {
		return gcpSecretAuth{useADC: true}, nil
	}
	tok, err := fallback.Token()
	if err != nil {
		return gcpSecretAuth{}, fmt.Errorf("no usable Google credentials: Application Default Credentials could not be used (%v) and the gcloud CLI fallback failed (%v); run `gcloud auth login` and retry", adcErr, err)
	}
	return gcpSecretAuth{
		tokenSource:  oauth2.ReuseTokenSource(tok, fallback),
		quotaProject: quotaProjectOfSecret(secretName),
	}, nil
}

// quotaProjectOfSecret extracts the project id from a fully-qualified secret
// version resource name ("projects/<project>/secrets/..."). It returns ""
// when the name does not follow that form.
func quotaProjectOfSecret(secretName string) string {
	rest, ok := strings.CutPrefix(secretName, "projects/")
	if !ok {
		return ""
	}
	project, _, ok := strings.Cut(rest, "/")
	if !ok {
		return ""
	}
	return project
}

// gcloudCLITokenSource mints Google access tokens by invoking the gcloud CLI,
// using the credentials from `gcloud auth login`. It serves as a fallback for
// developer workstations where Application Default Credentials were never set
// up.
type gcloudCLITokenSource struct {
	// run is injectable for testing. When nil, the real gcloud CLI is invoked.
	run func(ctx context.Context) ([]byte, error)
}

func (s *gcloudCLITokenSource) Token() (*oauth2.Token, error) {
	run := s.run
	if run == nil {
		run = runGcloudPrintAccessToken
	}
	out, err := run(context.Background())
	if err != nil {
		return nil, fmt.Errorf("minting access token via gcloud CLI: %w", err)
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return nil, errors.New("gcloud CLI returned an empty access token")
	}
	return &oauth2.Token{AccessToken: token}, nil
}

func runGcloudPrintAccessToken(ctx context.Context) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "gcloud", "auth", "print-access-token")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("running `gcloud auth print-access-token`: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
