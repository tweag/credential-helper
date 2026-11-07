package artifactkeeper

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/tweag/credential-helper/api"
)

func basicHeader(userPass string) map[string][]string {
	return map[string][]string{
		"Authorization": {"Basic " + base64.StdEncoding.EncodeToString([]byte(userPass))},
	}
}

func TestResolverGet(t *testing.T) {
	const uri = "https://artifacts.example.com/maven/central/com/example/lib/1.0/lib-1.0.jar"
	tests := []struct {
		name       string
		credential func(context.Context) (string, error)
		want       map[string][]string
		wantErr    bool
	}{
		{
			name:       "basic auth from user:token",
			credential: func(context.Context) (string, error) { return "svc-reader:ak_token", nil },
			want:       basicHeader("svc-reader:ak_token"),
		},
		{
			name:       "no header when the credential is empty",
			credential: func(context.Context) (string, error) { return "", nil },
		},
		{
			name:       "rejects a credential without a user",
			credential: func(context.Context) (string, error) { return "tokenwithoutcolon", nil },
			wantErr:    true,
		},
		{
			name:       "surfaces other lookup errors",
			credential: func(context.Context) (string, error) { return "", errors.New("keyring locked") },
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Resolver{credential: tt.credential}
			resp, err := r.Get(context.Background(), api.GetCredentialsRequest{URI: uri})
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Get() = nil error, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Get() returned unexpected error: %v", err)
			}
			if diff := cmp.Diff(tt.want, resp.Headers); diff != "" {
				t.Errorf("Get() headers mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestResolverFromEnv(t *testing.T) {
	const uri = "https://artifacts.example.com/npm/registry/left-pad/-/left-pad-1.3.0.tgz"

	t.Setenv("ARTIFACT_KEEPER_CREDENTIAL", "svc-reader:ak_token")
	res, err := (&ArtifactKeeper{}).Resolver(context.Background())
	if err != nil {
		t.Fatalf("Resolver() error: %v", err)
	}
	resp, err := res.Get(context.Background(), api.GetCredentialsRequest{URI: uri})
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if diff := cmp.Diff(basicHeader("svc-reader:ak_token"), resp.Headers); diff != "" {
		t.Errorf("Get() headers mismatch (-want +got):\n%s", diff)
	}

	// A machine without the credential gets an anonymous request rather than a
	// failed download, so Bazel can fall back to the next URL on a 401.
	t.Setenv("ARTIFACT_KEEPER_CREDENTIAL", "")
	res, err = (&ArtifactKeeper{}).Resolver(context.Background())
	if err != nil {
		t.Fatalf("Resolver() error: %v", err)
	}
	resp, err = res.Get(context.Background(), api.GetCredentialsRequest{URI: uri})
	if err != nil {
		t.Fatalf("Get() with no credential returned error: %v", err)
	}
	if len(resp.Headers) != 0 {
		t.Errorf("Get() with no credential returned headers %v, want none", resp.Headers)
	}
}

func TestCacheKey(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want string
	}{
		{name: "host", uri: "https://artifacts.example.com/go/proxy/golang.org/x/text/@v/v0.20.0.zip", want: "artifacts.example.com"},
		{name: "host with port", uri: "https://artifacts.example.com:8443/general/files/x.tar.gz", want: "artifacts.example.com:8443"},
		{name: "empty on unparseable uri", uri: "://not-a-url", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := (&ArtifactKeeper{}).CacheKey(api.GetCredentialsRequest{URI: tt.uri})
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("CacheKey(%q) mismatch (-want +got):\n%s", tt.uri, diff)
			}
		})
	}
}
