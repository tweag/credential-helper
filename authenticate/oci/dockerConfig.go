package oci

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tweag/credential-helper/logging"
)

type DockerAuthConfig struct {
	Username      string `json:"username,omitempty"`
	Password      string `json:"password,omitempty"`
	Auth          string `json:"auth,omitempty"`
	ServerAddress string `json:"serveraddress,omitempty"`
	IdentityToken string `json:"identitytoken,omitempty"`
	RegistryToken string `json:"registrytoken,omitempty"`
}

func (a DockerAuthConfig) toAuthConfig() (AuthConfig, error) {
	if a.Password == "" && a.Auth != "" {
		username, password, err := decodeAuthField(a.Auth)
		if err != nil {
			return AuthConfig{}, err
		}

		a.Username = username
		a.Password = password
	} else if a.Auth == "" && a.Username != "" && a.Password != "" {
		// We need to encode the username and password into the auth field.
		a.Auth = encodeAuthField(a.Username, a.Password)

	}

	return AuthConfig{
		Username:      a.Username,
		Password:      a.Password,
		Auth:          a.Auth,
		IdentityToken: a.IdentityToken,
		RegistryToken: a.RegistryToken,
	}, nil
}

type CredentialHelperOutput struct {
	ServerURL string `json:"ServerURL"`
	Username  string `json:"Username"`
	Secret    string `json:"Secret"`
}

func (c CredentialHelperOutput) toAuthConfig() (AuthConfig, error) {
	return AuthConfig{
		Username: c.Username,
		Password: c.Secret,
	}, nil
}

// ConfigFile represents the Docker configuration file.
// It is limited to the fields we need.
type ConfigFile struct {
	Auths             map[string]DockerAuthConfig `json:"auths,omitempty"`
	CredentialsStore  string                      `json:"credsStore,omitempty"`
	CredentialHelpers map[string]string           `json:"credHelpers,omitempty"`
}

func loadDockerConfig() (ConfigFile, error) {
	home, err := os.UserHomeDir()
	var haveHomeDir bool
	if err == nil {
		haveHomeDir = true
	}
	dockerConfigEnv, haveDockerConfigEnv := os.LookupEnv("DOCKER_CONFIG")
	registryAuthEnv, haveRegistryAuthEnv := os.LookupEnv("REGISTRY_AUTH_FILE")
	runtimeDir, haveRuntimeDir := os.LookupEnv("XDG_RUNTIME_DIR")

	locations := []string{}
	if haveHomeDir {
		locations = append(locations, filepath.Join(home, ".docker", "config.json"))
	}
	if haveDockerConfigEnv {
		locations = append(locations, filepath.Join(dockerConfigEnv, "config.json"))
	}
	if haveRegistryAuthEnv {
		locations = append(locations, registryAuthEnv)
	}
	if haveRuntimeDir {
		locations = append(locations, filepath.Join(runtimeDir, "containers", "auth.json"))
	}

	for _, loc := range locations {
		f, err := os.Open(loc)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return ConfigFile{}, err
		}
		defer f.Close()

		var cfg ConfigFile
		if err := json.NewDecoder(f).Decode(&cfg); err != nil {
			return ConfigFile{}, err
		}
		return cfg, nil
	}

	// No config file found
	return ConfigFile{}, nil
}

func authForHost(cfg ConfigFile, host string) (authCfg AuthConfig, found bool, err error) {
	// the precedence is:
	// 1. if credHelpers contains the host, use it
	// 2. if credsStore is set, use it with the host
	// 3. if auths contains the host, use it

	if helper, ok := cfg.CredentialHelpers[host]; ok {
		return dockerCredentialHelperToAuth(helper, host)
	}

	if cfg.CredentialsStore != "" {
		return dockerCredentialHelperToAuth(cfg.CredentialsStore, host)
	}

	auth, ok := cfg.Auths[host]
	if !ok {
		return AuthConfig{}, false, nil
	}
	out, err := auth.toAuthConfig()
	if err != nil {
		return AuthConfig{}, false, err
	}
	logging.Debugf("using credentials from config file for host %s", host)
	return out, true, nil
}

func decodeAuthField(authField string) (string, string, error) {
	decodedAuth, err := base64.RawStdEncoding.DecodeString(authField)
	if err != nil {
		return "", "", err
	}

	parts := strings.SplitN(string(decodedAuth), ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid auth field: %s", authField)
	}

	return parts[0], parts[1], nil
}

func encodeAuthField(username, password string) string {
	auth := username + ":" + password
	return base64.RawStdEncoding.EncodeToString([]byte(auth))
}

func dockerCredentialHelperToAuth(helper, host string) (authCfg AuthConfig, found bool, err error) {
	cmd := exec.Command("docker-credential-"+helper, "get")
	cmd.Stdin = bytes.NewBufferString(host)
	out, err := cmd.Output()
	if err != nil {
		if strings.Contains(string(out), "credentials not found in native keychain") {
			logging.Debugf("docker-credential-%s get did not find credentials, continuing without authentication: %v", helper, string(out))
			return AuthConfig{}, false, nil
		}
		return AuthConfig{}, false, fmt.Errorf("running helper docker-credential-%s get: %v", helper, err)
	}

	var cred CredentialHelperOutput
	if err := json.Unmarshal(out, &cred); err != nil {
		return AuthConfig{}, false, err
	}

	authCfg, err = cred.toAuthConfig()
	if err != nil {
		return AuthConfig{}, false, err
	}
	logging.Debugf("using credentials from helper docker-credential-%s for host %s", helper, host)

	return authCfg, true, nil
}
