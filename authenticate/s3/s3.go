package authenticate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	signerv4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/tweag/credential-helper/api"
)

const (
	expiresIn   = 15 * time.Minute
	emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

type R2 struct{}

func (R2) Resolver(ctx context.Context) (api.Resolver, error) {
	accessKeyID, ok := os.LookupEnv("R2_ACCESS_KEY_ID")
	if !ok {
		return nil, errors.New("R2_ACCESS_KEY_ID not set")
	}
	// try to use secret access key directly
	secretAccessKey, ok := os.LookupEnv("R2_SECRET_ACCESS_KEY")
	if !ok {
		// try to use cloudflare token
		cloudflareToken, ok := os.LookupEnv("CLOUDFLARE_API_TOKEN")
		if !ok {
			return nil, errors.New("need R2_SECRET_ACCESS_KEY or R2_SECRET_ACCESS_KEY environment variables to access R2")
		}
		// cloudflare token can be hashed to obtain the secret access key for the S3 API
		hasher := sha256.New()
		hasher.Write([]byte(cloudflareToken))
		secretAccessKey = hex.EncodeToString(hasher.Sum(nil))
	}

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, "")),
		config.WithRegion("auto"),
	)
	if err != nil {
		return nil, err
	}

	return &S3Resolver{
		signer: signerv4.NewSigner(),
		config: cfg,
	}, nil
}

// CacheKey returns the cache key for the given request.
// For R2, every object has a unique signature, so the URI is a good cache key.
func (r *R2) CacheKey(req api.GetCredentialsRequest) string {
	return req.URI
}

type S3 struct{}

func (S3) Resolver(ctx context.Context) (api.Resolver, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	return &S3Resolver{
		signer: signerv4.NewSigner(),
		config: cfg,
	}, nil
}

// CacheKey returns the cache key for the given request.
// For S3, every object has a unique signature, so the URI is a good cache key.
func (s *S3) CacheKey(req api.GetCredentialsRequest) string {
	return req.URI
}

type S3Resolver struct {
	signer signerv4.HTTPSigner
	config aws.Config
}

// Get implements the get command of the credential-helper spec:
//
// https://github.com/EngFlow/credential-helper-spec/blob/main/spec.md#get
func (s *S3Resolver) Get(ctx context.Context, req api.GetCredentialsRequest) (api.GetCredentialsResponse, error) {
	parsedURL, err := url.Parse(req.URI)
	if err != nil {
		return api.GetCredentialsResponse{}, err
	}

	if parsedURL.Scheme != "https" {
		return api.GetCredentialsResponse{}, errors.New("only https is supported")
	}

	region := regionFromHost(parsedURL.Host)
	if region == "" {
		return api.GetCredentialsResponse{}, errors.New("unable to determine region from host")
	}

	s3provider := providerFromHost(parsedURL.Host)
	if s3provider == ProviderUnknown {
		return api.GetCredentialsResponse{}, errors.New("unsupported S3 backend")
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

	cred, err := s.config.Credentials.Retrieve(ctx)
	if err != nil {
		return api.GetCredentialsResponse{}, err
	}

	ts := time.Now().UTC()

	if err := s.signer.SignHTTP(ctx, cred, &httpReq, emptySHA256, "s3", region, ts); err != nil {
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
