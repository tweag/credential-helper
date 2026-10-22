package googlesecretmanager

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

type GoogleSecretManager struct{}

// CacheKey returns a cache key for the given request.
// For Google Secret Manager, the same token can be used for all requests.
func (g *GoogleSecretManager) CacheKey(req api.GetCredentialsRequest) string {
	return "https://secretmanager.googleapis.com/"
}

func (g *GoogleSecretManager) SetupInstructionsForURI(ctx context.Context, uri string) string {
	return fmt.Sprintf(`%s is a Google Secret Manager URL.

IAM Setup:

  In order to access a secret, you need a Google Cloud user- or service account with read access to the secret versions you want to access (secretmanager.versions.access, contained in the role roles/secretmanager.secretAccessor). No other permissions are needed. You can grant access to a single secret with:

    $ gcloud secrets add-iam-policy-binding <SECRET_NAME> \
        --project=<PROJECT> \
        --member=user:<EMAIL> \
        --role=roles/secretmanager.secretAccessor

  Refer to Google's documentation for more information: https://cloud.google.com/secret-manager/docs/access-control

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

func (GoogleSecretManager) Resolver(ctx context.Context) (api.Resolver, error) {
	credentials, err := gauth.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, err
	}
	return &GoogleSecretManagerResolver{tokenSource: credentials.TokenSource}, nil
}

type GoogleSecretManagerResolver struct {
	tokenSource oauth2.TokenSource
}

// Get implements the get command of the credential-helper spec:
//
// https://github.com/EngFlow/credential-helper-spec/blob/main/spec.md#get
func (g *GoogleSecretManagerResolver) Get(ctx context.Context, req api.GetCredentialsRequest) (api.GetCredentialsResponse, error) {
	parsedURL, err := url.Parse(req.URI)
	if err != nil {
		return api.GetCredentialsResponse{}, err
	}

	if parsedURL.Scheme != "https" {
		return api.GetCredentialsResponse{}, errors.New("only https is supported")
	}

	if !strings.EqualFold(parsedURL.Hostname(), "secretmanager.googleapis.com") {
		return api.GetCredentialsResponse{}, fmt.Errorf("only secretmanager.googleapis.com URLs are supported but provided: %v", parsedURL)
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
