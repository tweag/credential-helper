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
		if errors.Is(err, notFoundError) {
			continue
		}
		return "", fmt.Errorf("looking up binding %q: %w", binding, err)
	}
	sourceNames := make([]string, len(c.config))
	for i, entry := range c.config {
		sourceNames[i] = entry.Source
	}
	return "", fmt.Errorf("no value found for binding %q after querying %v", binding, strings.Join(sourceNames, ", "))
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
		return "", notFoundError
	}
	val, ok := os.LookupEnv(e.Name)
	if !ok {
		return "", notFoundError
	}
	return val, nil
}

func (e *Env) Canonicalize() {
	e.Source = "env"
	if e.Binding == "" {
		e.Binding = "default"
	}
}

type Keyring struct {
	// Source is the name of the source used to look up the secret.
	// It must be "keyring".
	Source string `json:"source"`
	// Key is the name of the key to look up in the keyring.
	Key string `json:"key"`
	// Binding binds the value of the keyring secret to a well-known name in the helper.
	// If not specified, the value is bound to the default secret of the helper.
	Binding string `json:"binding,omitempty"`
}

func (k *Keyring) Lookup(binding string) (string, error) {
	if k.Binding != binding {
		return "", notFoundError
	}
	val, err := keyring.Get("gh:github.com", "")
	if errors.Is(err, keyring.ErrNotFound) {
		return "", notFoundError
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

var notFoundError = errors.New("not found")
