package registry

import (
	"github.com/tweag/credential-helper/api"
	authenticateGCS "github.com/tweag/credential-helper/authenticate/gcs"
	authenticateGitHub "github.com/tweag/credential-helper/authenticate/github"
	authenticateNull "github.com/tweag/credential-helper/authenticate/null"
	authenticateOCI "github.com/tweag/credential-helper/authenticate/oci"
	authenticateRemoteAPIs "github.com/tweag/credential-helper/authenticate/remoteapis"
	authenticateS3 "github.com/tweag/credential-helper/authenticate/s3"
)

var singleton = Helpers{
	Map: map[string]api.Helper{
		"gcs":        &authenticateGCS.GCS{},
		"github":     &authenticateGitHub.GitHub{},
		"null":       &authenticateNull.Null{},
		"oci":        authenticateOCI.NewFallbackOCI(),
		"remoteapis": &authenticateRemoteAPIs.RemoteAPIs{},
		"s3":         &authenticateS3.S3{},
	},
}

// HelperFromString returns the helper corresponding to the given string.
func HelperFromString(s string) api.Helper {
	return singleton.Map[s]
}

// Register registers a new helper with the given name.
func Register(name string, helper api.Helper) {
	singleton.register(name, helper)
}

// Names returns the names of all registered helpers.
func Names() []string {
	return singleton.names()
}

type Helpers struct {
	Map map[string]api.Helper
}

func (h *Helpers) register(name string, helper api.Helper) {
	h.Map[name] = helper
}

func (h *Helpers) names() []string {
	names := make([]string, 0, len(h.Map))
	for name := range h.Map {
		names = append(names, name)
	}
	return names
}
