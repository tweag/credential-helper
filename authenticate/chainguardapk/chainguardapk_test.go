package chainguardapk

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/tweag/credential-helper/api"
)

func basicHeader(userPass string) map[string][]string {
	return map[string][]string{
		"Authorization": {"Basic " + base64.StdEncoding.EncodeToString([]byte(userPass))},
	}
}

func TestChainguardAPKResolverGet(t *testing.T) {
	// A Chainguard pull-token credential is "<identity-id>:<token>" userinfo; the
	// identity id is the org UIDP plus a per-identity suffix and contains a slash.
	const cred = "0123456789abcdef0123456789abcdef01234567/89abcdef01234567:tok123"
	tests := []struct {
		name       string
		uri        string
		credential func(context.Context) (string, error)
		want       map[string][]string
		wantErr    bool
	}{
		{
			name:       "basic auth for apk host",
			uri:        "https://apk.cgr.dev/example-org/x86_64/APKINDEX.tar.gz",
			credential: func(context.Context) (string, error) { return cred, nil },
			want:       basicHeader(cred),
		},
		{
			name:       "basic auth is case-insensitive on host",
			uri:        "https://APK.CGR.DEV/example-org/x86_64/foo.apk",
			credential: func(context.Context) (string, error) { return cred, nil },
			want:       basicHeader(cred),
		},
		{
			name:       "rejects foreign host",
			uri:        "https://example.com/x",
			credential: func(context.Context) (string, error) { return cred, nil },
			wantErr:    true,
		},
		{
			name:       "surfaces credential lookup error",
			uri:        "https://apk.cgr.dev/example-org/x86_64/foo.apk",
			credential: func(context.Context) (string, error) { return "", context.DeadlineExceeded },
			wantErr:    true,
		},
		{
			name:       "rejects credential missing identity id",
			uri:        "https://apk.cgr.dev/example-org/x86_64/foo.apk",
			credential: func(context.Context) (string, error) { return "tokenwithoutcolon", nil },
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ChainguardAPKResolver{credential: tt.credential}
			resp, err := r.Get(context.Background(), api.GetCredentialsRequest{URI: tt.uri})
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Get(%q) = nil error, want error", tt.uri)
				}
				return
			}
			if err != nil {
				t.Fatalf("Get(%q) returned unexpected error: %v", tt.uri, err)
			}
			if diff := cmp.Diff(tt.want, resp.Headers); diff != "" {
				t.Errorf("Get(%q) headers mismatch (-want +got):\n%s", tt.uri, diff)
			}
		})
	}
}

func TestChainguardLibrariesResolverGet(t *testing.T) {
	const cred = "0123456789abcdef0123456789abcdef01234567/89abcdef01234567:tok123"
	r := &ChainguardAPKResolver{
		host:       NewLibraries().host,
		credential: func(context.Context) (string, error) { return cred, nil },
	}
	for _, uri := range []string{
		"https://libraries.cgr.dev/javascript/picocolors/-/picocolors-1.1.1.tgz",
		"https://libraries.cgr.dev/javascript-upstream/@scope/pkg/-/pkg-1.0.0.tgz",
	} {
		resp, err := r.Get(context.Background(), api.GetCredentialsRequest{URI: uri})
		if err != nil {
			t.Fatalf("Get(%q) returned unexpected error: %v", uri, err)
		}
		if diff := cmp.Diff(basicHeader(cred), resp.Headers); diff != "" {
			t.Errorf("Get(%q) headers mismatch (-want +got):\n%s", uri, diff)
		}
	}
	// The libraries credential must never be sent to the apk host, or the
	// reverse.
	for _, uri := range []string{
		"https://apk.cgr.dev/example-org/x86_64/foo.apk",
		"https://example.com/javascript/x.tgz",
	} {
		if _, err := r.Get(context.Background(), api.GetCredentialsRequest{URI: uri}); err == nil {
			t.Errorf("Get(%q) = nil error, want error", uri)
		}
	}
	apk := &ChainguardAPKResolver{credential: func(context.Context) (string, error) { return cred, nil }}
	if _, err := apk.Get(context.Background(), api.GetCredentialsRequest{URI: "https://libraries.cgr.dev/javascript/x.tgz"}); err == nil {
		t.Error("apk resolver accepted libraries.cgr.dev, want error")
	}
}

func TestChainguardResolversReadTheirOwnEnv(t *testing.T) {
	t.Setenv("CGR_APK_CREDENTIAL", "apk-id:apk-token")
	t.Setenv("CGR_LIBRARIES_CREDENTIAL", "lib-id:lib-token")
	for _, tt := range []struct {
		helper *ChainguardAPK
		uri    string
		want   string
	}{
		{&ChainguardAPK{}, "https://apk.cgr.dev/o/x86_64/foo.apk", "apk-id:apk-token"},
		{NewLibraries(), "https://libraries.cgr.dev/javascript/x.tgz", "lib-id:lib-token"},
	} {
		res, err := tt.helper.Resolver(context.Background())
		if err != nil {
			t.Fatalf("Resolver() error: %v", err)
		}
		resp, err := res.Get(context.Background(), api.GetCredentialsRequest{URI: tt.uri})
		if err != nil {
			t.Fatalf("Get(%q) error: %v", tt.uri, err)
		}
		if diff := cmp.Diff(basicHeader(tt.want), resp.Headers); diff != "" {
			t.Errorf("Get(%q) headers mismatch (-want +got):\n%s", tt.uri, diff)
		}
	}
}

// Each libraries.cgr.dev repository takes its own pull token, so cached
// credentials are keyed by repository as well as host.
func TestChainguardLibrariesCacheKey(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want string
	}{
		{name: "javascript", uri: "https://libraries.cgr.dev/javascript/picocolors/-/picocolors-1.1.1.tgz", want: "libraries.cgr.dev/javascript"},
		{name: "javascript upstream", uri: "https://libraries.cgr.dev/javascript-upstream/x/-/x-1.0.0.tgz", want: "libraries.cgr.dev/javascript-upstream"},
		{name: "java", uri: "https://libraries.cgr.dev/java/com/google/guava/guava/33.0.0-jre/guava-33.0.0-jre.jar", want: "libraries.cgr.dev/java"},
		{name: "go", uri: "https://libraries.cgr.dev/go/golang.org/x/text/@v/v0.20.0.zip", want: "libraries.cgr.dev/go"},
		{name: "empty on unparseable uri", uri: "://not-a-url", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewLibraries().CacheKey(api.GetCredentialsRequest{URI: tt.uri})
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("CacheKey(%q) mismatch (-want +got):\n%s", tt.uri, diff)
			}
		})
	}
}

func TestChainguardAPKCacheKey(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want string
	}{
		{name: "host from apk url", uri: "https://apk.cgr.dev/example-org/x86_64/x.apk", want: "apk.cgr.dev"},
		{name: "empty on unparseable uri", uri: "://not-a-url", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &ChainguardAPK{}
			got := c.CacheKey(api.GetCredentialsRequest{URI: tt.uri})
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("CacheKey(%q) mismatch (-want +got):\n%s", tt.uri, diff)
			}
		})
	}
}
