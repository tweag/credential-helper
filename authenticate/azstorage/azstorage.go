package azstorage

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/tweag/credential-helper/api"
	"golang.org/x/oauth2"
)

type AzStorage struct{}

// CacheKey returns a cache key for the given request.
// For AzStorage, we use the URI which include the project and repo names.
func (g *AzStorage) CacheKey(req api.GetCredentialsRequest) string {

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

func (g *AzStorage) SetupInstructionsForURI(ctx context.Context, uri string) string {
	return fmt.Sprintf(`%s is a Azure Storage URL.

IAM Setup:

To access blob data, a user must have the following role assignments:
- A data access role, such as Storage Blob Data Reader or Storage Blob Data Contributor
- The Azure Resource Manager Reader role, at a minimum.

The following example assigns the Storage Blob Data Reader role to a user by specifying the object ID.
In this example, the role assignment is scoped to the level of the storage account.

az role assignment create \
   --role "Storage Blob Data Reader" \
	--assignee-object-id "aaaaaaaa-0000-1111-2222-bbbbbbbbbbbb" \
	--assignee-principal-type "User" \
	--scope "/subscriptions/<subscription-id>/resourceGroups/<resource-group-name>/providers/Microsoft.Storage/storageAccounts/<storage-account-name>"

Refer to Azure's documentation for more information about RBAC: https://learn.microsoft.com/en-us/azure/role-based-access-control/

Authentication Methods:

There are several methods that you can use to authenticate with Azure.

You can refer to Azure's documentation: https://learn.microsoft.com/en-us/cli/azure/authenticate-azure-cli?view=azure-cli-latest`, uri)
}

// AzureTokenSource implements oauth2.TokenSource by providing the Token() method.
type AzureTokenSource struct {
	cred   azcore.TokenCredential
	scopes []string
}

func (ts *AzureTokenSource) Token() (*oauth2.Token, error) {
	ctx := context.Background()

	azToken, err := ts.cred.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: ts.scopes,
	})

	if err != nil {
		return nil, err
	}

	return &oauth2.Token{
		AccessToken: azToken.Token,
		TokenType:   "Bearer",
		Expiry:      azToken.ExpiresOn,
	}, nil
}

func (AzStorage) Resolver(ctx context.Context) (api.Resolver, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, err
	}

	tokenSource := &AzureTokenSource{
		cred:   cred,
		scopes: []string{"https://storage.azure.com/.default"},
	}

	return &AzStorageResolver{tokenSource: tokenSource}, nil
}

type AzStorageResolver struct {
	tokenSource oauth2.TokenSource
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return fallback
	}
	return value
}

// Get implements the get command of the credential-helper spec:
//
// https://github.com/EngFlow/credential-helper-spec/blob/main/spec.md#get
func (az *AzStorageResolver) Get(ctx context.Context, req api.GetCredentialsRequest) (api.GetCredentialsResponse, error) {
	parsedURL, error := url.Parse(req.URI)
	if error != nil {
		return api.GetCredentialsResponse{}, error
	}

	if parsedURL.Scheme != "https" {
		return api.GetCredentialsResponse{}, errors.New("only https is supported")
	}

	if !strings.HasSuffix(parsedURL.Hostname(), "blob.core.windows.net") {
		return api.GetCredentialsResponse{}, fmt.Errorf("only blob.core.windows.net URLs are supported but provided: %v", parsedURL)
	}

	if parsedURL.Port() != "" && parsedURL.Port() != "443" {
		return api.GetCredentialsResponse{}, errors.New("only port 443 is supported")
	}

	token, err := az.tokenSource.Token()
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
			"x-ms-version":  {getenv("AZURE_HEADER_X_MS_VERSION", "2025-07-05")},
		},
	}, nil
}
