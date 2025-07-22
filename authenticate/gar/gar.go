package gar

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/tweag/credential-helper/api"
	"golang.org/x/oauth2"
	gauth "golang.org/x/oauth2/google"
)

type GAR struct{}

// CacheKey returns a cache key for the given request.
// For GAR, we use the URI which include the project and repo names.
func (g *GAR) CacheKey(req api.GetCredentialsRequest) string {

	parsed, err := url.Parse(req.URI)
	if err != nil {
		return req.URI
	}

	paths := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
	if len(paths) > 2 {
		paths = paths[:2]
	}

	parsed.Path = "/" + strings.Join(paths, "/")

	return parsed.String()
}

func (g *GAR) SetupInstructionsForURI(ctx context.Context, uri string) string {
	return fmt.Sprintf(`%s is a Google Artifact Registry URL.

IAM Setup:

  In order to access packages from a registry, you need a Google Cloud user- or service account with read access to the repos you want to access (e.g IAM role with read access roles/artifactregistry.reader).
  No other permissions are needed. Refer to Google's documentation for more information: https://cloud.google.com/storage/docs/access-control/iam-permissions

Authentication Methods:

  Option 1: Using gcloud CLI as a regular user (Recommended)
    1. Install the Google Cloud SDK: https://cloud.google.com/sdk/docs/install
    2. Run:
      $ gcloud auth application-default login
    3. Follow the browser prompts to authenticate

  Option 2: Using a Service Account Key, OpenID Connect or other authentication mechanisms
    1. Follow Google's documentation for choosing and setting up your method of choice: https://cloud.google.com/docs/authentication
    2. Ensure your method of choice sets the Application Default Credentials (ADC) environment variable (GOOGLE_APPLICATION_CREDENTIALS): https://cloud.google.com/docs/authentication/provide-credentials-adc
    3. Alternatively, check that the credentials file is in a well-known location ($HOME/.config/gcloud/application_default_credentials.json)`, uri)
}

func (GAR) Resolver(ctx context.Context) (api.Resolver, error) {
	credentials, err := gauth.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform.read-only")
	if err != nil {
		return nil, err
	}
	return &GARResolver{tokenSource: credentials.TokenSource}, nil
}

type GARResolver struct {
	tokenSource oauth2.TokenSource
}

// Get implements the get command of the credential-helper spec:
//
// https://github.com/EngFlow/credential-helper-spec/blob/main/spec.md#get
func (g *GARResolver) Get(ctx context.Context, req api.GetCredentialsRequest) (api.GetCredentialsResponse, error) {
	parsedURL, error := url.Parse(req.URI)
	if error != nil {
		return api.GetCredentialsResponse{}, error
	}

	if parsedURL.Scheme != "https" {
		return api.GetCredentialsResponse{}, errors.New("only https is supported")
	}

	if !strings.HasSuffix(parsedURL.Hostname(), "pkg.dev") {
		return api.GetCredentialsResponse{}, fmt.Errorf("only pkg.dev URLs are supported but provided: %v", parsedURL)
	}

	if parsedURL.Port() != "" && parsedURL.Port() != "443" {
		return api.GetCredentialsResponse{}, errors.New("only port 443 is supported")
	}

	token, err := g.tokenSource.Token()
	if err != nil {
		return api.GetCredentialsResponse{}, err
	}
	var expires string
	if !token.Expiry.IsZero() {
		expires = token.Expiry.UTC().Format(time.RFC3339)
	}
	return api.GetCredentialsResponse{
		Expires: expires,
		Headers: map[string][]string{
			"Authorization": {"Bearer " + token.AccessToken},
		},
	}, nil
}
