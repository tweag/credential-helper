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
