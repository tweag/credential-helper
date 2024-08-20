package authenticate

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	signerv4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/tweag/credential-helper/api"
)

const expiresIn = 15 * time.Minute

type S3 struct {
	signer signerv4.HTTPSigner
	config aws.Config
}

func New(ctx context.Context) (*S3, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	return &S3{
		// TODO: maybe disable header hoisting if needed
		// func(opts *signerv4.SignerOptions) {
		//	opts.DisableHeaderHoisting = true
		//}
		signer: signerv4.NewSigner(),
		config: cfg,
	}, nil
}

// Get implements the get command of the credential-helper spec:
//
// https://github.com/EngFlow/credential-helper-spec/blob/main/spec.md#get
func (s *S3) Get(ctx context.Context, req api.GetCredentialsRequest) (api.GetCredentialsResponse, error) {
	parsedURL, error := url.Parse(req.URI)
	if error != nil {
		return api.GetCredentialsResponse{}, error
	}

	if parsedURL.Scheme != "https" {
		return api.GetCredentialsResponse{}, errors.New("only https is supported")
	}

	region := regionFromHost(parsedURL.Host)
	if region == "" {
		return api.GetCredentialsResponse{}, errors.New("unable to determine region from host")
	}

	httpReq := http.Request{
		Method: http.MethodGet,
		URL:    parsedURL,
	}

	cred, err := s.config.Credentials.Retrieve(ctx)
	if err != nil {
		return api.GetCredentialsResponse{}, err
	}

	ts := time.Now().UTC()

	if err := s.signer.SignHTTP(ctx, cred, &httpReq, "UNSIGNED-PAYLOAD", "s3", region, ts); err != nil {
		return api.GetCredentialsResponse{}, err
	}

	return api.GetCredentialsResponse{
		Expires: ts.Add(expiresIn).Format(time.RFC3339),
		Headers: httpReq.Header,
	}, nil
}

// CacheKey returns the cache key for the given request.
// For S3, every object has a unique signature, so the URI is a good cache key.
func (s *S3) CacheKey(req api.GetCredentialsRequest) string {
	return req.URI
}

func regionFromHost(host string) string {
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
	// virtual-hosted-style url
	// <bucket>.<region>.s3.amazonaws.com
	host = strings.TrimSuffix(host, ".s3.amazonaws.com")

	parts := strings.Split(host, ".")
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}
