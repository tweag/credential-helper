package string

import (
	"github.com/tweag/credential-helper/api"

	authenticateGCS "github.com/tweag/credential-helper/authenticate/gcs"
	authenticateGitHub "github.com/tweag/credential-helper/authenticate/github"
	authenticateNull "github.com/tweag/credential-helper/authenticate/null"
	authenticateOCI "github.com/tweag/credential-helper/authenticate/oci"
	authenticateS3 "github.com/tweag/credential-helper/authenticate/s3"
)

func HelperFromString(s string) api.Helper {
	switch s {
	case "s3":
		return &authenticateS3.S3{}
	case "gcs":
		return &authenticateGCS.GCS{}
	case "github":
		return &authenticateGitHub.GitHub{}
	case "oci":
		return authenticateOCI.NewFallbackOCI()
	case "null":
		return &authenticateNull.Null{}
	}
	return nil
}
