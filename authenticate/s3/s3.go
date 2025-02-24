package authenticate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	signerv4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/tweag/credential-helper/api"
	"github.com/tweag/credential-helper/authenticate/internal/helperconfig"
	"github.com/tweag/credential-helper/authenticate/internal/lookupchain"
)

const (
	expiresIn   = 15 * time.Minute
	emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

type S3 struct{}

func (S3) Resolver(ctx context.Context) (api.Resolver, error) {
	return &S3Resolver{
		signer: signerv4.NewSigner(),
	}, nil
}

func (g *S3) SetupInstructionsForURI(ctx context.Context, uri string) string {
	parsedURL, error := url.Parse(uri)
	if error != nil {
		parsedURL = &url.URL{}
	}

	var lookupChainInstructions []string
	cfg, err := configFromContext(ctx, parsedURL)
	if err == nil {
		chain := lookupchain.New(cfg.LookupChain)
		lookupChainInstructions = append(lookupChainInstructions, chain.SetupInstructions(BindigAccessKeyID, "AWS Access Key ID"))
		lookupChainInstructions = append(lookupChainInstructions, chain.SetupInstructions(BindingSecretAccessKey, "AWS Secret Access Key"))
		lookupChainInstructions = append(lookupChainInstructions, chain.SetupInstructions(BindingSessionToken, "AWS Session Token"))
		lookupChainInstructions = append(lookupChainInstructions, chain.SetupInstructions(BindingRegion, "AWS Region"))
		if providerFromHost(parsedURL.Host) == ProviderCloudflareR2 {
			lookupChainInstructions = append(lookupChainInstructions, chain.SetupInstructions(BindingCloudflareAPIToken, "Cloudflare API Token - can optionally be used to derive the secret access key"))
		}
	} else {
		lookupChainInstructions = []string{fmt.Sprintf("due to a configuration parsing issue, no further setup instructions are available: %v", err)}
	}

	var serviceString string
	switch providerFromHost(parsedURL.Host) {
	case ProviderAWS:
		serviceString = "AWS S3 object"
	case ProviderCloudflareR2:
		serviceString = "Cloudflare R2 object"
	default:
		serviceString = "S3-compatible object store object"
	}

	return fmt.Sprintf(`%s is a %s.

The credential helper can be used to download objects from S3 (or S3-compatible object store) buckets.

IAM Setup:

  In order to access data from a bucket, you need an AWS user- or service account with read access to the objects you want to access (s3:GetObject).
  Refer to the AWS documentation for more information: https://docs.aws.amazon.com/AmazonS3/latest/userguide/security-iam.html

Authentication Methods:

  Option 1: Using the AWS CLI and Single Sign On (SSO) as a regular user (Recommended)
    1. Install the AWS CLI: https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html
    2. Follow the documentation for using aws configure sso and aws sso login to sign in: https://docs.aws.amazon.com/signin/latest/userguide/command-line-sign-in.html
    3. Follow the browser prompts to authenticate

  Option 2: Authenticate with other methods:
    AWS has a lot of ways to authenticate and the credential helper uses the official SDK.
    If you have more complex requirements, follow the AWS documentation to setup your method of choice:

      https://docs.aws.amazon.com/sdkref/latest/guide/access.html

    This may require you to set environment variables when using Bazel (or other tools).

%s`, uri, serviceString, strings.Join(lookupChainInstructions, "\n\n"))
}

// CacheKey returns the cache key for the given request.
// For S3, every object has a unique signature, so the URI is a good cache key.
func (s *S3) CacheKey(req api.GetCredentialsRequest) string {
	return req.URI
}

type S3Resolver struct {
	signer signerv4.HTTPSigner
}

// Get implements the get command of the credential-helper spec:
//
// https://github.com/EngFlow/credential-helper-spec/blob/main/spec.md#get
func (s *S3Resolver) Get(ctx context.Context, req api.GetCredentialsRequest) (api.GetCredentialsResponse, error) {
	parsedURL, err := url.Parse(req.URI)
	if err != nil {
		return api.GetCredentialsResponse{}, err
	}

	if parsedURL.Query().Has("X-Amz-Expires") {
		// This is a presigned URL, no need to sign it again.
		return api.GetCredentialsResponse{}, nil
	}

	if parsedURL.Scheme != "https" {
		return api.GetCredentialsResponse{}, errors.New("only https is supported")
	}

	cfg, err := configFromContext(ctx, parsedURL)
	if err != nil {
		return api.GetCredentialsResponse{}, fmt.Errorf("getting configuration fragment for remotapis helper and url %s: %w", req.URI, err)
	}

	chain := lookupchain.New(cfg.LookupChain)

	var accessKeyID, secretAccessKey, sessionToken, region string

	if cfg.Region != "" {
		region = cfg.Region
	}

	accessKeyIDLookup, err := chain.Lookup(BindigAccessKeyID)
	if err == nil {
		accessKeyID = accessKeyIDLookup
	} else if !lookupchain.IsNotFoundErr(err) {
		return api.GetCredentialsResponse{}, err
	}

	if providerFromHost(parsedURL.Host) == ProviderCloudflareR2 {
		// cloudflare token can be hashed to obtain the secret access key for the S3 API
		cloudflareAPIToken, err := chain.Lookup(BindingCloudflareAPIToken)
		if err == nil {
			hasher := sha256.New()
			hasher.Write([]byte(cloudflareAPIToken))
			secretAccessKey = hex.EncodeToString(hasher.Sum(nil))
		}
		if err != nil && !lookupchain.IsNotFoundErr(err) {
			return api.GetCredentialsResponse{}, err
		}
	}

	secretAccessKeyLookup, err := chain.Lookup(BindingSecretAccessKey)
	if err == nil {
		secretAccessKey = secretAccessKeyLookup
	} else if !lookupchain.IsNotFoundErr(err) {
		return api.GetCredentialsResponse{}, err
	}

	sessionTokenLookup, err := chain.Lookup(BindingSessionToken)
	if err == nil {
		sessionToken = sessionTokenLookup
	} else if lookupchain.IsNotFoundErr(err) {
		logging.Debugf("session token lookup: binding %s did not yield any secrets - continuing without", BindingSessionToken)
	} else {
		logging.Debugf("session token lookup failed - continuing without: %v", err)
	}

	regionLookup, err := chain.Lookup(BindingRegion)
	if err == nil {
		region = regionLookup
	} else if !lookupchain.IsNotFoundErr(err) {
		return api.GetCredentialsResponse{}, err
	}

	var awsConfigOptions []func(*config.LoadOptions) error

	if len(accessKeyID) > 0 && len(secretAccessKey) > 0 {
		awsConfigOptions = append(awsConfigOptions,
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, sessionToken)))
	}

	if len(region) > 0 {
		awsConfigOptions = append(awsConfigOptions,
			config.WithRegion(region))
	}

	awsConfig, err := config.LoadDefaultConfig(ctx, awsConfigOptions...)
	if err != nil {
		return api.GetCredentialsResponse{}, err
	}

	httpReq := http.Request{
		Method: http.MethodGet,
		URL:    parsedURL,
		Header: map[string][]string{
			// We assume this is a GET request, so the request body must be empty.
			// The SHA-256 hash of an empty string is always e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855.
			"X-Amz-Content-SHA256": {emptySHA256},
		},
	}

	cred, err := awsConfig.Credentials.Retrieve(ctx)
	if err != nil {
		return api.GetCredentialsResponse{}, err
	}

	ts := time.Now().UTC()

	if err := s.signer.SignHTTP(ctx, cred, &httpReq, emptySHA256, "s3", cfg.Region, ts); err != nil {
		return api.GetCredentialsResponse{}, err
	}

	return api.GetCredentialsResponse{
		Expires: ts.Add(expiresIn).Format(time.RFC3339),
		Headers: httpReq.Header,
	}, nil
}

func regionFromHost(host string) string {
	// cloudfare r2
	if strings.HasSuffix(host, ".r2.cloudflarestorage.com") {
		return "auto"
	}

	// AWS S3
	if host == "s3.amazonaws.com" {
		return "us-east-1"
	}

	if strings.HasPrefix(host, "s3.") && strings.HasSuffix(host, ".amazonaws.com") {
		// path-style url
		// s3.<region>.amazonaws.com
		parts := strings.Split(host, ".")
		if len(parts) != 4 {
			return ""
		}
		return parts[1]
	}

	if !strings.HasSuffix(host, ".s3.amazonaws.com") {
		return ""
	}
	host = strings.TrimSuffix(host, ".s3.amazonaws.com")

	parts := strings.Split(host, ".")
	switch len(parts) {
	case 1:
		// virtual-hosted-style url
		// <bucket>.s3.amazonaws.com
		return "us-east-1"
	case 2:
		// virtual-hosted-style url
		// <bucket>.<region>.s3.amazonaws.com
		return parts[1]
	}

	return ""
}

type S3Provider int

const (
	ProviderUnknown S3Provider = iota
	ProviderAWS
	ProviderCloudflareR2
)

func providerFromHost(host string) S3Provider {
	if strings.HasSuffix(host, ".r2.cloudflarestorage.com") {
		return ProviderCloudflareR2
	}

	if strings.HasSuffix(host, ".amazonaws.com") {
		return ProviderAWS
	}

	return ProviderUnknown
}

const (
	BindigAccessKeyID         = "aws-access-key-id"
	BindingSecretAccessKey    = "aws-secret-access-key"
	BindingSessionToken       = "aws-session-token"
	BindingCloudflareAPIToken = "cloudflare-api-token"
	BindingRegion             = "aws-default-region"
)

type configFragment struct {
	// Region is the AWS region to use.
	// If not set, the region is determined automatically.
	Region string `json:"region"`
	// LookupChain defines the order in which secrets are looked up from sources.
	// Each element is a string that identifies a secret source.
	// It defaults to the sources "env", "keyring".
	LookupChain lookupchain.Config `json:"lookup_chain"`
}

func configFromContext(ctx context.Context, uri *url.URL) (configFragment, error) {
	sources := []lookupchain.Source{
		// acces key id
		&lookupchain.Env{
			Source:  "env",
			Name:    "AWS_ACCESS_KEY_ID",
			Binding: BindigAccessKeyID,
		},
		&lookupchain.Keyring{
			Source:  "env",
			Service: "tweag-credential-helper:aws-access-key-id",
			Binding: BindigAccessKeyID,
		},

		// secret access key
		&lookupchain.Env{
			Source:  "env",
			Name:    "AWS_SECRET_ACCESS_KEY",
			Binding: BindingSecretAccessKey,
		},
		&lookupchain.Keyring{
			Source:  "env",
			Service: "tweag-credential-helper:aws-secret-access-key",
			Binding: BindingSecretAccessKey,
		},

		// default region
		&lookupchain.Env{
			Source:  "env",
			Name:    "AWS_DEFAULT_REGION",
			Binding: BindingRegion,
		},
		&lookupchain.Keyring{
			Source:  "env",
			Service: "tweag-credential-helper:aws-default-region",
			Binding: BindingRegion,
		},
	}

	var cfg configFragment

	switch providerFromHost(uri.Host) {
	case ProviderAWS:
		cfg.Region = regionFromHost(uri.Host)
	case ProviderCloudflareR2:
		cfg.Region = "auto"
		sources = append([]lookupchain.Source{
			&lookupchain.Env{
				Source:  "env",
				Name:    "R2_ACCESS_KEY_ID",
				Binding: BindigAccessKeyID,
			},
			&lookupchain.Env{
				Source:  "env",
				Name:    "R2_SECRET_ACCESS_KEY",
				Binding: BindingSecretAccessKey,
			},
			&lookupchain.Env{
				Source:  "env",
				Name:    "CLOUDFLARE_API_TOKEN",
				Binding: BindingCloudflareAPIToken,
			},
			&lookupchain.Keyring{
				Source:  "env",
				Service: "tweag-credential-helper:cloudflare-api-token",
				Binding: BindingCloudflareAPIToken,
			},
		}, sources...)
	}

	cfg.LookupChain = lookupchain.Default(sources)

	return helperconfig.FromContext(ctx, cfg)
}
