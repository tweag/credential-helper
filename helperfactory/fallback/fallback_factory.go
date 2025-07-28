package fallback

import (
	"net/url"
	"strings"

	"github.com/tweag/credential-helper/api"
	authenticateGAR "github.com/tweag/credential-helper/authenticate/gar"
	authenticateGCS "github.com/tweag/credential-helper/authenticate/gcs"
	authenticateGitHub "github.com/tweag/credential-helper/authenticate/github"
	authenticateNull "github.com/tweag/credential-helper/authenticate/null"
	authenticateOCI "github.com/tweag/credential-helper/authenticate/oci"
	authenticateRemoteAPIs "github.com/tweag/credential-helper/authenticate/remoteapis"
	authenticateS3 "github.com/tweag/credential-helper/authenticate/s3"
	"github.com/tweag/credential-helper/logging"
)

func FallbackHelperFactory(rawURL string) (api.Helper, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	switch {
	case strings.HasSuffix(u.Hostname(), ".amazonaws.com"):
		return &authenticateS3.S3{}, nil
	case strings.EqualFold(u.Hostname(), "storage.googleapis.com"):
		return &authenticateGCS.GCS{}, nil
	case strings.EqualFold(u.Hostname(), "github.com"):
		fallthrough
	case strings.HasSuffix(strings.ToLower(u.Host), ".github.com"):
		fallthrough
	case strings.EqualFold(u.Hostname(), "raw.githubusercontent.com"):
		return &authenticateGitHub.GitHub{}, nil
	case strings.HasSuffix(strings.ToLower(u.Hostname()), ".r2.cloudflarestorage.com") && !u.Query().Has("X-Amz-Expires"):
		return &authenticateS3.S3{}, nil
	case strings.HasSuffix(u.Hostname(), ".buildbuddy.io"):
		return &authenticateRemoteAPIs.RemoteAPIs{}, nil
	case strings.HasSuffix(u.Hostname(), "pkg.dev"):
		return &authenticateGAR.GAR{}, nil
	// container registries using the default OCI resolver
	case strings.HasSuffix(u.Hostname(), ".app.snowflake.com"):
		fallthrough
	case strings.HasSuffix(u.Hostname(), ".azurecr.io"):
		fallthrough
	case strings.EqualFold(u.Hostname(), "cgr.dev"):
		fallthrough
	case strings.EqualFold(u.Hostname(), "docker.elastic.co"):
		fallthrough
	case strings.EqualFold(u.Hostname(), "gcr.io"):
		fallthrough
	case strings.EqualFold(u.Hostname(), "ghcr.io"):
		fallthrough
	case strings.EqualFold(u.Hostname(), "index.docker.io"):
		fallthrough
	case strings.EqualFold(u.Hostname(), "nvcr.io"):
		fallthrough
	case strings.EqualFold(u.Hostname(), "public.ecr.aws"):
		fallthrough
	case strings.EqualFold(u.Hostname(), "quay.io"):
		fallthrough
	case strings.EqualFold(u.Hostname(), "registry.gitlab.com"):
		return authenticateOCI.NewFallbackOCI(), nil
	default:
		if authenticateOCI.GuessOCIRegistry(rawURL) {
			logging.Debugf("$CREDENTIAL_HELPER_GUESS_OCI_REGISTRY is set and uri looks like a registry: %s\n", rawURL)
			return authenticateOCI.NewFallbackOCI(), nil
		}
		logging.Basicf("no matching credential helper found for %s - returning empty response\n", rawURL)
		return authenticateNull.Null{}, nil
	}
}
