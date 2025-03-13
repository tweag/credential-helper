package gcs

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/tweag/credential-helper/api"
	"golang.org/x/oauth2"
	gauth "golang.org/x/oauth2/google"
)

type GCS struct{}

// CacheKey returns a cache key for the given request.
// For GCS, the same token can be used for all requests.
func (g *GCS) CacheKey(req api.GetCredentialsRequest) string {
	return "https://storage.googleapis.com/"
}

func (g *GCS) SetupInstructionsForURI(ctx context.Context, uri string) string {
	return fmt.Sprintf(`%s is a Google Cloud Storage (GCS) url.

IAM Setup:

  In order to access data from a bucket, you need a Google Cloud user- or service account with read access to the objects you want to access (storage.objects.get).
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

func (GCS) Resolver(ctx context.Context) (api.Resolver, error) {
	credentials, err := gauth.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/devstorage.read_only")
	if err != nil {
		return nil, err
	}
	return &GCSResolver{tokenSource: credentials.TokenSource}, nil
}

type GCSResolver struct {
	tokenSource oauth2.TokenSource
}

// Get implements the get command of the credential-helper spec:
//
// https://github.com/EngFlow/credential-helper-spec/blob/main/spec.md#get
func (g *GCSResolver) Get(ctx context.Context, req api.GetCredentialsRequest) (api.GetCredentialsResponse, error) {
	parsedURL, error := url.Parse(req.URI)
	if error != nil {
		return api.GetCredentialsResponse{}, error
	}

	if parsedURL.Scheme != "https" {
		return api.GetCredentialsResponse{}, errors.New("only https is supported")
	}

	if parsedURL.Hostname() != "storage.googleapis.com" {
		return api.GetCredentialsResponse{}, errors.New("only storage.googleapis.com is supported")
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
