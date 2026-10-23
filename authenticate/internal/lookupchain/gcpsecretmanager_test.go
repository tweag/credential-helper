package lookupchain

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/oauth2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGCPSecretManagerLookup(t *testing.T) {
	const secretName = "projects/p/secrets/apk/versions/latest"

	tests := []struct {
		name           string
		binding        string // binding the source is configured for
		queryBinding   string // binding passed to Lookup
		secret         string
		accessorResult string
		accessorErr    error
		want           string
		wantNotFound   bool
		wantErr        bool
		// wantAccessedName is the secret name the accessor is expected to be
		// called with; empty means the accessor must NOT be called.
		wantAccessedName string
	}{
		{
			name:             "returns payload for matching binding",
			binding:          "default",
			queryBinding:     "default",
			secret:           secretName,
			accessorResult:   "s3cr3t-token",
			want:             "s3cr3t-token",
			wantAccessedName: secretName,
		},
		{
			name:         "skips non-matching binding without calling accessor",
			binding:      "default",
			queryBinding: "other",
			secret:       secretName,
			wantNotFound: true,
		},
		{
			name:             "surfaces accessor error",
			binding:          "default",
			queryBinding:     "default",
			secret:           secretName,
			accessorErr:      errors.New("permission denied"),
			wantErr:          true,
			wantAccessedName: secretName,
		},
		{
			name:         "errors when secret is unset",
			binding:      "default",
			queryBinding: "default",
			secret:       "",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotName string
			called := false
			s := &GCPSecretManager{
				Source:  "gcp_secretmanager",
				Secret:  tt.secret,
				Binding: tt.binding,
				accessor: func(_ context.Context, name string) (string, error) {
					called = true
					gotName = name
					return tt.accessorResult, tt.accessorErr
				},
			}
			s.Canonicalize()

			got, err := s.Lookup(tt.queryBinding)

			switch {
			case tt.wantNotFound:
				if !IsNotFoundErr(err) {
					t.Fatalf("Lookup(%q) error = %v, want NotFoundErr", tt.queryBinding, err)
				}
			case tt.wantErr:
				if err == nil {
					t.Fatalf("Lookup(%q) = nil error, want error", tt.queryBinding)
				}
			default:
				if err != nil {
					t.Fatalf("Lookup(%q) unexpected error: %v", tt.queryBinding, err)
				}
				if diff := cmp.Diff(tt.want, got); diff != "" {
					t.Errorf("Lookup(%q) value mismatch (-want +got):\n%s", tt.queryBinding, diff)
				}
			}

			if tt.wantAccessedName == "" {
				if called {
					t.Errorf("accessor was called with %q, want not called", gotName)
				}
				return
			}
			if diff := cmp.Diff(tt.wantAccessedName, gotName); diff != "" {
				t.Errorf("accessor secret name mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGcloudCLITokenSource(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		runErr  error
		want    string
		wantErr string
	}{
		{
			name:   "trims trailing newline from token output",
			output: "ya29.gcloud-token\n",
			want:   "ya29.gcloud-token",
		},
		{
			name:    "errors on empty output",
			output:  "\n",
			wantErr: "empty access token",
		},
		{
			name:    "wraps gcloud invocation error",
			runErr:  errors.New("exec: \"gcloud\": executable file not found in $PATH"),
			wantErr: "gcloud",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := &gcloudCLITokenSource{
				run: func(context.Context) ([]byte, error) {
					return []byte(tt.output), tt.runErr
				},
			}
			tok, err := ts.Token()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Token() error = %v, want error containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Token() unexpected error: %v", err)
			}
			if diff := cmp.Diff(tt.want, tok.AccessToken); diff != "" {
				t.Errorf("Token() access token mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestQuotaProjectOfSecret(t *testing.T) {
	tests := []struct {
		name   string
		secret string
		want   string
	}{
		{
			name:   "extracts project from fully-qualified secret version name",
			secret: "projects/example-project/secrets/my-secret/versions/latest",
			want:   "example-project",
		},
		{
			name:   "empty for malformed name",
			secret: "not-a-secret-name",
			want:   "",
		},
		{
			name:   "empty for bare projects prefix",
			secret: "projects/p",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, quotaProjectOfSecret(tt.secret)); diff != "" {
				t.Errorf("quotaProjectOfSecret(%q) mismatch (-want +got):\n%s", tt.secret, diff)
			}
		})
	}
}

type failingTokenSource struct{ err error }

func (f *failingTokenSource) Token() (*oauth2.Token, error) { return nil, f.err }

func TestChooseGCPSecretAuth(t *testing.T) {
	const secretName = "projects/example-project/secrets/my-secret/versions/latest"
	ctx := context.Background()

	t.Run("uses ADC when available without consulting fallback", func(t *testing.T) {
		fallback := &failingTokenSource{err: errors.New("fallback must not be used")}
		auth, err := chooseGCPSecretAuth(ctx, secretName, nil, fallback)
		if err != nil {
			t.Fatalf("chooseGCPSecretAuth() unexpected error: %v", err)
		}
		if !auth.useADC {
			t.Errorf("useADC = false, want true")
		}
		if auth.tokenSource != nil {
			t.Errorf("tokenSource = %v, want nil when using ADC", auth.tokenSource)
		}
	})

	t.Run("falls back to gcloud CLI credentials when ADC is unavailable", func(t *testing.T) {
		fallback := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "gcloud-user-token"})
		auth, err := chooseGCPSecretAuth(ctx, secretName, errors.New("google: could not find default credentials"), fallback)
		if err != nil {
			t.Fatalf("chooseGCPSecretAuth() unexpected error: %v", err)
		}
		if auth.useADC {
			t.Errorf("useADC = true, want false")
		}
		if auth.tokenSource == nil {
			t.Fatal("tokenSource = nil, want fallback-backed token source")
		}
		tok, err := auth.tokenSource.Token()
		if err != nil {
			t.Fatalf("tokenSource.Token() unexpected error: %v", err)
		}
		if diff := cmp.Diff("gcloud-user-token", tok.AccessToken); diff != "" {
			t.Errorf("access token mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff("example-project", auth.quotaProject); diff != "" {
			t.Errorf("quota project mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("reports actionable error when ADC and gcloud both fail", func(t *testing.T) {
		fallback := &failingTokenSource{err: errors.New("no gcloud login")}
		_, err := chooseGCPSecretAuth(ctx, secretName, errors.New("google: could not find default credentials"), fallback)
		if err == nil {
			t.Fatal("chooseGCPSecretAuth() = nil error, want error")
		}
		if !strings.Contains(err.Error(), "gcloud auth login") {
			t.Errorf("error %q does not mention `gcloud auth login`", err)
		}
	})
}

func TestAccessGCPSecretWithFallback(t *testing.T) {
	const secretName = "projects/example-project/secrets/my-secret/versions/latest"
	ctx := context.Background()
	deniedErr := status.Error(codes.PermissionDenied, "Permission 'secretmanager.versions.access' denied on resource")

	t.Run("returns payload from ADC without consulting fallback", func(t *testing.T) {
		fallback := &failingTokenSource{err: errors.New("fallback must not be used")}
		var calls []gcpSecretAuth
		got, err := accessGCPSecretWithFallback(ctx, secretName, nil, fallback, func(_ context.Context, _ string, auth gcpSecretAuth) (string, error) {
			calls = append(calls, auth)
			return "adc-payload", nil
		})
		if err != nil {
			t.Fatalf("accessGCPSecretWithFallback() unexpected error: %v", err)
		}
		if got != "adc-payload" {
			t.Errorf("payload = %q, want %q", got, "adc-payload")
		}
		if len(calls) != 1 || !calls[0].useADC {
			t.Errorf("calls = %+v, want exactly one ADC access", calls)
		}
	})

	t.Run("retries with gcloud CLI credentials when ADC is denied", func(t *testing.T) {
		fallback := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "gcloud-user-token"})
		var calls []gcpSecretAuth
		got, err := accessGCPSecretWithFallback(ctx, secretName, nil, fallback, func(_ context.Context, _ string, auth gcpSecretAuth) (string, error) {
			calls = append(calls, auth)
			if auth.useADC {
				return "", deniedErr
			}
			return "gcloud-payload", nil
		})
		if err != nil {
			t.Fatalf("accessGCPSecretWithFallback() unexpected error: %v", err)
		}
		if got != "gcloud-payload" {
			t.Errorf("payload = %q, want %q", got, "gcloud-payload")
		}
		if len(calls) != 2 || !calls[0].useADC || calls[1].useADC {
			t.Fatalf("calls = %+v, want ADC access followed by fallback access", calls)
		}
		tok, err := calls[1].tokenSource.Token()
		if err != nil {
			t.Fatalf("fallback tokenSource.Token() unexpected error: %v", err)
		}
		if diff := cmp.Diff("gcloud-user-token", tok.AccessToken); diff != "" {
			t.Errorf("fallback access token mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff("example-project", calls[1].quotaProject); diff != "" {
			t.Errorf("fallback quota project mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("does not retry on non-permission errors", func(t *testing.T) {
		fallback := &failingTokenSource{err: errors.New("fallback must not be used")}
		notFound := status.Error(codes.NotFound, "secret not found")
		calls := 0
		_, err := accessGCPSecretWithFallback(ctx, secretName, nil, fallback, func(context.Context, string, gcpSecretAuth) (string, error) {
			calls++
			return "", notFound
		})
		if !errors.Is(err, notFound) {
			t.Fatalf("accessGCPSecretWithFallback() error = %v, want %v", err, notFound)
		}
		if calls != 1 {
			t.Errorf("access calls = %d, want 1", calls)
		}
	})

	t.Run("reports both errors when ADC is denied and gcloud login is missing", func(t *testing.T) {
		fallback := &failingTokenSource{err: errors.New("no gcloud login")}
		_, err := accessGCPSecretWithFallback(ctx, secretName, nil, fallback, func(context.Context, string, gcpSecretAuth) (string, error) {
			return "", deniedErr
		})
		if err == nil {
			t.Fatal("accessGCPSecretWithFallback() = nil error, want error")
		}
		if !strings.Contains(err.Error(), "gcloud auth login") {
			t.Errorf("error %q does not mention `gcloud auth login`", err)
		}
	})

	t.Run("surfaces fallback denial after ADC denial", func(t *testing.T) {
		fallback := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "gcloud-user-token"})
		_, err := accessGCPSecretWithFallback(ctx, secretName, nil, fallback, func(context.Context, string, gcpSecretAuth) (string, error) {
			return "", deniedErr
		})
		if !errors.Is(err, deniedErr) {
			t.Fatalf("accessGCPSecretWithFallback() error = %v, want %v", err, deniedErr)
		}
	})
}

func TestGCPSecretManagerSetupInstructions(t *testing.T) {
	s := &GCPSecretManager{
		Secret:  "projects/p/secrets/apk/versions/latest",
		Binding: "default",
	}
	s.Canonicalize()
	got, ok := s.SetupInstructions("default")
	if !ok {
		t.Fatal("SetupInstructions() ok = false, want true")
	}
	if !strings.Contains(got, "gcloud auth login") {
		t.Errorf("SetupInstructions() = %q, want mention of `gcloud auth login`", got)
	}
}
