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
	"reflect"
	"strings"
	"time"

	keyring "github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
	gauth "golang.org/x/oauth2/google"
	idtoken "google.golang.org/api/idtoken"
	option "google.golang.org/api/option"
)

const (
	SourceEnv     = "env"
	SourceKeyring = "keyring"
	SourceStatic  = "static"
	SourceGoogle  = "google"
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
		errs = append(errs, fmt.Errorf("source %q: %w", entry.Source, err))
	}
	sourceNames := make([]string, len(c.config))
	for i, entry := range c.config {
		sourceNames[i] = entry.Source
	}

	if len(errs) > 0 {
		return "", fmt.Errorf("no value found for binding %q after querying: %w", binding, errors.Join(errs...))
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
