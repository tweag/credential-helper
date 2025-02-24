package lookupchain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	keyring "github.com/zalando/go-keyring"
)

const (
	SourceEnv     = "env"
	SourceKeyring = "keyring"
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
		return "", fmt.Errorf("looking up binding %q: %w", binding, err)
	}
	sourceNames := make([]string, len(c.config))
	for i, entry := range c.config {
		sourceNames[i] = entry.Source
	}
	return "", &NotFoundErr{reason: fmt.Sprintf("no value found for binding %q after querying %v", binding, strings.Join(sourceNames, ", "))}
}

func (c *LookupChain) SetupInstructions(binding, meaning string) string {
	if len(c.config) == 0 {
		return fmt.Sprintf("No sources are configured to look up the secret with binding name %s. Refer to the documentation related to configuration files for more information.", binding)
	}
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
