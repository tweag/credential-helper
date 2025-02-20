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
	authenticateNull "github.com/tweag/credential-helper/authenticate/null"
	helperstringfactory "github.com/tweag/credential-helper/helperfactory/string"
)

var ErrConfigNotFound = errors.New("config file not found")

type URLConfig struct {
	Scheme string `json:"scheme,omitempty"`
	Host   string `json:"host,omitempty"`
	Path   string `json:"path,omitempty"`
	Helper string `json:"helper"`
}

type Config struct {
	URLs []URLConfig `json:"urls,omitempty"`
}

func (c Config) FindHelper(uri string) (api.Helper, error) {
	requested, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	if len(c.URLs) == 0 {
		return nil, errors.New("invalid configuration file: no helpers configured")
	}
	for _, url := range c.URLs {
		// if a scheme is specified, it must match
		if len(url.Scheme) > 0 && url.Scheme != requested.Scheme {
			continue
		}
		// if a host is specified, it must glob match
		if len(url.Host) > 0 && !globMatch(url.Host, requested.Host) {
			continue
		}
		// if a path is specified, it must glob match
		if len(url.Path) > 0 && !globMatch(url.Path, requested.Path) {
			continue
		}
		helper := helperstringfactory.HelperFromString(url.Helper)
		if helper != nil {
			return helper, nil
		}
		return nil, fmt.Errorf("unknown helper: %s", url.Helper)
	}
	return &authenticateNull.Null{}, nil
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
