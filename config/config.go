package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/tweag/credential-helper/agent/locate"
	"github.com/tweag/credential-helper/api"
	helperstringfactory "github.com/tweag/credential-helper/helperfactory/string"
	"github.com/tweag/credential-helper/logging"
)

var ErrConfigNotFound = errors.New("config file not found")

type URLConfig struct {
	Scheme string          `json:"scheme,omitempty"`
	Host   string          `json:"host,omitempty"`
	Path   string          `json:"path,omitempty"`
	Helper string          `json:"helper"`
	Config json.RawMessage `json:"config,omitempty"` // the schema of this field is defined by the helper
}

type Config struct {
	URLs []URLConfig `json:"urls,omitempty"`
}

func (c Config) FindHelper(uri string) (api.Helper, []byte, error) {
	requested, err := url.Parse(uri)
	if err != nil {
		return nil, nil, err
	}
	if len(c.URLs) == 0 {
		return nil, nil, errors.New("invalid configuration file: no helpers configured")
	}
	for _, urlConfig := range c.URLs {
		if len(urlConfig.Helper) == 0 {
			return nil, nil, errors.New("invalid configuration file: helper field is required")
		}

		// if a scheme is specified, it must match
		if len(urlConfig.Scheme) > 0 && urlConfig.Scheme != requested.Scheme {
			continue
		}
		// if a host is specified, it must glob match
		if len(urlConfig.Host) > 0 && !globMatch(urlConfig.Host, requested.Host) {
			continue
		}
		// if a path is specified, it must glob match
		if len(urlConfig.Path) > 0 && !globMatch(urlConfig.Path, requested.Path) {
			continue
		}
		helper := helperstringfactory.HelperFromString(urlConfig.Helper)
		if helper != nil {
			logging.Debugf("selected helper %s from config", urlConfig.Helper)
			return helper, urlConfig.Config, nil
		}
		return nil, nil, fmt.Errorf("unknown helper: %s", urlConfig.Helper)
	}
	// this is equivalent to null.Null{}
	// but avoids the import of the null package
	return helperstringfactory.HelperFromString("null"), nil, nil
}

type ConfigReader interface {
	Read() (Config, error)
}

type OSReader struct{}

func (r OSReader) Read() (Config, error) {
	configPath := locate.LookupPathEnv(api.ConfigFileEnv, filepath.Join("%workspace%", ".tweag-credential-helper.json"), false)
	file, err := os.Open(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, ErrConfigNotFound
		}
		return Config{}, err
	}
	defer file.Close()

	var config Config
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&config)
	if err != nil {
		return Config{}, err
	}

	return config, nil
}

func globMatch(pattern, candidate string) bool {
	patternIDX := 0
	candidateIDX := 0
	nextPatternIDX := 0
	nextCandidateIDX := 0
	for patternIDX < len(pattern) || candidateIDX < len(candidate) {
		if patternIDX < len(pattern) {
			c := pattern[patternIDX]
			switch c {
			default: // ordinary character
				if candidateIDX < len(candidate) && candidate[candidateIDX] == c {
					patternIDX++
					candidateIDX++
					continue
				}
			case '*': // zero-or-more-character wildcard
				nextPatternIDX = patternIDX
				nextCandidateIDX = candidateIDX + 1
				patternIDX++
				continue
			}
		}
		if 0 < nextCandidateIDX && nextCandidateIDX <= len(candidate) {
			patternIDX = nextPatternIDX
			candidateIDX = nextCandidateIDX
			continue
		}
		return false
	}
	return true
}
