package fallback

import (
	"net/url"
	"strings"

	"github.com/tweag/credential-helper/api"
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
	case strings.HasSuffix(u.Host, ".amazonaws.com"):
		return &authenticateS3.S3{}, nil
	case strings.EqualFold(u.Host, "storage.googleapis.com"):
		return &authenticateGCS.GCS{}, nil
	case strings.EqualFold(u.Host, "github.com"):
		fallthrough
	case strings.HasSuffix(strings.ToLower(u.Host), ".github.com"):
		fallthrough
	case strings.EqualFold(u.Host, "raw.githubusercontent.com"):
		return &authenticateGitHub.GitHub{}, nil
	case strings.EqualFold(u.Host, "ghcr.io"):
		return authenticateGitHub.GitHubContainerRegistry(), nil
	case strings.HasSuffix(strings.ToLower(u.Host), ".r2.cloudflarestorage.com") && !u.Query().Has("X-Amz-Expires"):
		return &authenticateS3.S3{}, nil
	case strings.EqualFold(u.Host, "remote.buildbuddy.io"):
		return &authenticateRemoteAPIs.RemoteAPIs{}, nil
	// container registries using the default OCI resolver
	case strings.EqualFold(u.Host, "index.docker.io"):
		fallthrough
	case strings.EqualFold(u.Host, "public.ecr.aws"):
		fallthrough
	case strings.EqualFold(u.Host, "cgr.dev"):
		fallthrough
	case strings.EqualFold(u.Host, "registry.gitlab.com"):
		fallthrough
	case strings.EqualFold(u.Host, "docker.elastic.co"):
		fallthrough
	case strings.EqualFold(u.Host, "quay.io"):
		fallthrough
	case strings.EqualFold(u.Host, "nvcr.io"):
		fallthrough
	case strings.HasSuffix(u.Host, ".azurecr.io"):
		fallthrough
	case strings.HasSuffix(u.Host, ".app.snowflake.com"):
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
