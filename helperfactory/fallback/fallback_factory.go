package fallback

import (
	"net/url"
	"strings"

	"github.com/tweag/credential-helper/api"
	authenticateGCS "github.com/tweag/credential-helper/authenticate/gcs"
	authenticateGitHub "github.com/tweag/credential-helper/authenticate/github"
	authenticateNull "github.com/tweag/credential-helper/authenticate/null"
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
	case strings.HasSuffix(strings.ToLower(u.Host), ".r2.cloudflarestorage.com"):
		return &authenticateS3.R2{}, nil
	default:
		logging.Basicf("no matching credential helper found for %s - returning empty response\n", rawURL)
		return authenticateNull.Null{}, nil
	}
}
