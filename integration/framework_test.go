package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"testing"

	"github.com/bazelbuild/rules_go/go/runfiles"
	"github.com/tweag/credential-helper/api"
)

type response struct{}

type TestBuilder struct {
	credentialHelperBinary string
	t                      *testing.T
}

func (builder *TestBuilder) WithHelperBinary(credentialHelperBinary string) *TestBuilder {
	builder.credentialHelperBinary = credentialHelperBinary
	return builder
}

func (builder *TestBuilder) WithDefaultHelperBinary() *TestBuilder {
	if os.Getenv("BAZEL_TEST") == "1" {
		runfileLocation, err := runfiles.Rlocation("_main/tweag-credential-helper_/tweag-credential-helper")
		if err != nil {
			panic(err)
		}
		builder.credentialHelperBinary = runfileLocation
	} else {
		builder.credentialHelperBinary = "tweag-credential-helper"
	}
	return builder
}

func (builder *TestBuilder) WithT(t *testing.T) *TestBuilder {
	builder.t = t
	return builder
}

func (builder *TestBuilder) Build() *CredentialHelperTest {
	return &CredentialHelperTest{
		credentialHelperBinary: builder.credentialHelperBinary,
		t:                      builder.t,
	}
}

type CredentialHelperTest struct {
	credentialHelperBinary string
	t                      *testing.T
}

func (test *CredentialHelperTest) invoke(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, test.credentialHelperBinary, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	return out, err
}

func (test *CredentialHelperTest) getCommand(ctx context.Context, uri string) (api.GetCredentialsResponse, error) {
	stdin, err := json.Marshal(api.GetCredentialsRequest{URI: uri})
	if err != nil {
		return api.GetCredentialsResponse{}, err
	}

	var response api.GetCredentialsResponse
	stdout, err := test.invoke(ctx, stdin, "get")
	if err != nil {
		return api.GetCredentialsResponse{}, err
	}
	if err := json.Unmarshal(stdout, &response); err != nil {
		return api.GetCredentialsResponse{}, err
	}
	return response, nil
}

func (test *CredentialHelperTest) Fetch(ctx context.Context, uri string) (*FetchResult, error) {
	helperResp, err := test.getCommand(ctx, uri)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, http.NoBody)
	if err != nil {
		return nil, err
	}

	for key, values := range helperResp.Headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	client := http.Client{}

	httpResp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}

	return &FetchResult{
		helperResp: &helperResp,
		httpResp:   httpResp,
		body:       body,
		t:          test.t,
	}, nil
}

type FetchResult struct {
	helperResp *api.GetCredentialsResponse
	httpResp   *http.Response
	body       []byte
	t          *testing.T
}

func (result *FetchResult) ExpectStatusCode(expected int) {
	if result.httpResp == nil {
		result.t.Error("http response is nil")
		return
	}
	if result.httpResp.StatusCode != expected {
		result.t.Errorf("expected status code %d, got %d", expected, result.httpResp.StatusCode)
	}
}

func (result *FetchResult) ExpectHeader(key, value string) {
	if result.httpResp == nil {
		result.t.Error("http response is nil")
		return
	}
	actual := result.httpResp.Header.Get(key)
	if actual != value {
		result.t.Errorf("expected header %q to be %q, got %q", key, value, actual)
	}
}

func (result *FetchResult) ExpectHelperToReturnHeader(headerName string, validations ...func([]string) error) {
	if result.helperResp == nil {
		result.t.Error("helper response is nil")
		return
	}
	actual := result.helperResp.Headers[headerName]
	for _, validation := range validations {
		if err := validation(actual); err != nil {
			result.t.Error(err)
		}
	}
}

func (result *FetchResult) ExpectBody(expected []byte) {
	if !bytes.Equal(result.body, expected) {
		result.t.Errorf("expected body to match %v, got %v", string(expected), string(result.body))
	}
}

func (result *FetchResult) ExpectBodySHA256(expected [sha256.Size]byte) {
	if result.httpResp == nil {
		result.t.Error("http response is nil")
		return
	}
	hash := sha256.New()
	hash.Write(result.body)
	actual := hash.Sum(nil)
	if !bytes.Equal(actual, expected[:]) {
		result.t.Errorf("expected body sha256 to match %x, got %x", expected, actual)
	}
}

func HexToSHA256(t *testing.T, hexStr string) [sha256.Size]byte {
	hash, err := hex.DecodeString(hexStr)
	if err != nil {
		t.Fatalf("failed to decode hex: %v", err)
	}
	var hash32 [sha256.Size]byte
	copy(hash32[:], hash)
	return hash32
}
