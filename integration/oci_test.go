package integration

import (
	"context"
	"strings"
	"testing"
)

func TestDockerHub(t *testing.T) {
	builder := &TestBuilder{}
	test := builder.WithDefaultHelperBinary().WithT(t).Build()
	ctx := context.Background()
	fetchResult, err := test.Fetch(ctx, "https://index.docker.io/v2/library/hello-world/blobs/sha256:d2c94e258dcb3c5ac2798d32e1249e42ef01cba4841c2234249495f87264ac5a")
	if err != nil {
		t.Fatalf("failed to fetch credentials: %v", err)
	}
	fetchResult.ExpectHelperToReturnHeader("Authorization", func(s []string) error {
		if len(s) != 1 {
			t.Errorf("expected exactly one Authorization header, got %d", len(s))
		}
		if len(s) == 1 && !strings.HasPrefix(s[0], "Bearer ") {
			t.Errorf("expected Authorization header to start with 'Bearer '")
		}
		return nil
	})
	fetchResult.ExpectStatusCode(200)
	fetchResult.ExpectBodySHA256(HexToSHA256(t, "d2c94e258dcb3c5ac2798d32e1249e42ef01cba4841c2234249495f87264ac5a"))
}
