package httpauth

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

func withConfig(t *testing.T, raw string) context.Context {
	t.Helper()
	return context.WithValue(context.Background(), api.HelperConfigKey, []byte(raw))
}

func get(t *testing.T, ctx context.Context, uri string) (map[string][]string, error) {
	t.Helper()
	resp, err := (&HTTP{}).Get(ctx, api.GetCredentialsRequest{URI: uri})
	return resp.Headers, err
}

func TestRespond(t *testing.T) {
	found := func(s string) func() (string, error) { return func() (string, error) { return s, nil } }
	tests := []struct {
		name    string
		cfg     configFragment
		lookup  func() (string, error)
		want    map[string][]string
		wantErr bool
	}{
		{
			name:   "basic auth",
			cfg:    configFragment{AuthMethod: AuthMethodBasic},
			lookup: found("svc-reader:token"),
			want:   basicHeader("svc-reader:token"),
		},
		{
			name:   "basic auth is the default",
			cfg:    configFragment{},
			lookup: found("svc-reader:token"),
			want:   basicHeader("svc-reader:token"),
		},
		{
			name:    "basic auth needs a user",
			cfg:     configFragment{AuthMethod: AuthMethodBasic},
			lookup:  found("tokenwithoutcolon"),
			wantErr: true,
		},
		{
			name:   "bearer",
			cfg:    configFragment{AuthMethod: AuthMethodBearer},
			lookup: found("tok"),
			want:   map[string][]string{"Authorization": {"Bearer tok"}},
		},
		{
			name:   "raw header",
			cfg:    configFragment{AuthMethod: AuthMethodHeader, HeaderName: "X-JFrog-Art-Api"},
			lookup: found("tok"),
			want:   map[string][]string{"X-JFrog-Art-Api": {"tok"}},
		},
		{
			name:    "raw header needs a name",
			cfg:     configFragment{AuthMethod: AuthMethodHeader},
			lookup:  found("tok"),
			wantErr: true,
		},
		{
			name:    "unknown auth method",
			cfg:     configFragment{AuthMethod: "digest"},
			lookup:  found("tok"),
			wantErr: true,
		},
		{
			name:   "allow_missing sends no header for an empty secret",
			cfg:    configFragment{AllowMissing: true},
			lookup: found(""),
		},
		{
			name:    "an empty secret is an error without allow_missing",
			cfg:     configFragment{},
			lookup:  found(""),
			wantErr: true,
		},
		{
			name:    "other lookup errors surface even with allow_missing",
			cfg:     configFragment{AllowMissing: true},
			lookup:  func() (string, error) { return "", errors.New("keyring locked") },
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := respond(tt.cfg, tt.lookup)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("respond() = nil error, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("respond() unexpected error: %v", err)
			}
			if diff := cmp.Diff(tt.want, resp.Headers); diff != "" {
				t.Errorf("headers mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetUsesTheLookupChain(t *testing.T) {
	t.Setenv("CREDENTIAL_HELPER_HTTP_SECRET", "user:pass")
	got, err := get(t, context.Background(), "https://repo.example.com/maven/x.jar")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if diff := cmp.Diff(basicHeader("user:pass"), got); diff != "" {
		t.Errorf("headers mismatch (-want +got):\n%s", diff)
	}
}

func TestGetAllowMissingFromConfig(t *testing.T) {
	ctx := withConfig(t, `{"allow_missing": true, "lookup_chain": [{"source": "env", "name": "TEST_HTTPAUTH_UNSET"}]}`)
	got, err := get(t, ctx, "https://artifacts.example.com/maven/central/x.jar")
	if err != nil {
		t.Fatalf("Get() with a missing secret and allow_missing returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Get() returned headers %v, want none", got)
	}

	ctx = withConfig(t, `{"lookup_chain": [{"source": "env", "name": "TEST_HTTPAUTH_UNSET"}]}`)
	if _, err := get(t, ctx, "https://artifacts.example.com/maven/central/x.jar"); err == nil {
		t.Error("Get() with a missing secret and no allow_missing = nil error, want error")
	}
}

func TestGetRejectsOtherSchemes(t *testing.T) {
	t.Setenv("CREDENTIAL_HELPER_HTTP_SECRET", "user:pass")
	if _, err := get(t, context.Background(), "grpcs://cache.example.com/build.bazel.remote.execution.v2.ActionCache"); err == nil {
		t.Error("Get() accepted a grpcs URI, want error")
	}
}

// Chainguard's hosts need no configuration: each reads its own environment
// variable, so the apk token is never sent to Libraries or the reverse.
func TestGetChainguardDefaults(t *testing.T) {
	t.Setenv("CGR_APK_CREDENTIAL", "apk-id:apk-token")
	t.Setenv("CGR_LIBRARIES_CREDENTIAL", "lib-id:lib-token")
	t.Setenv("CREDENTIAL_HELPER_HTTP_SECRET", "generic:secret")
	for _, tt := range []struct {
		uri  string
		want string
	}{
		{"https://apk.cgr.dev/example-org/x86_64/APKINDEX.tar.gz", "apk-id:apk-token"},
		{"https://APK.CGR.DEV/example-org/x86_64/foo.apk", "apk-id:apk-token"},
		{"https://libraries.cgr.dev/javascript/picocolors/-/picocolors-1.1.1.tgz", "lib-id:lib-token"},
		{"https://repo.example.com/x.tgz", "generic:secret"},
	} {
		got, err := get(t, context.Background(), tt.uri)
		if err != nil {
			t.Fatalf("Get(%q) error: %v", tt.uri, err)
		}
		if diff := cmp.Diff(basicHeader(tt.want), got); diff != "" {
			t.Errorf("Get(%q) headers mismatch (-want +got):\n%s", tt.uri, diff)
		}
	}
}

func TestGetConfigOverridesWellKnownDefaults(t *testing.T) {
	t.Setenv("CGR_LIBRARIES_JAVA", "java-id:java-token")
	ctx := withConfig(t, `{"lookup_chain": [{"source": "env", "name": "CGR_LIBRARIES_JAVA"}]}`)
	got, err := get(t, ctx, "https://libraries.cgr.dev/java/com/example/lib/1.0/lib-1.0.jar")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if diff := cmp.Diff(basicHeader("java-id:java-token"), got); diff != "" {
		t.Errorf("headers mismatch (-want +got):\n%s", diff)
	}
}

func TestCacheKey(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want string
	}{
		{name: "host and repository", uri: "https://libraries.cgr.dev/javascript/picocolors/-/picocolors-1.1.1.tgz", want: "libraries.cgr.dev/javascript"},
		{name: "a different repository on the same host", uri: "https://libraries.cgr.dev/java/com/google/guava/guava/33.0.0-jre/guava-33.0.0-jre.jar", want: "libraries.cgr.dev/java"},
		{name: "host with port", uri: "https://artifacts.example.com:8443/general/files/x.tar.gz", want: "artifacts.example.com:8443/general"},
		{name: "empty on unparseable uri", uri: "://not-a-url", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := (&HTTP{}).CacheKey(api.GetCredentialsRequest{URI: tt.uri})
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("CacheKey(%q) mismatch (-want +got):\n%s", tt.uri, diff)
			}
		})
	}
}
