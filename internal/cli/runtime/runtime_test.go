package runtime_test

import (
	"context"
	"testing"

	"github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestTokenResolverInterfacePropagatesContext(t *testing.T) {
	// Verify that TokenResolverFunc satisfies the TokenResolver interface with the new ctx signature.
	called := false
	var resolver runtime.TokenResolver = runtime.TokenResolverFunc(func(ctx context.Context, profile config.WorkspaceProfile) (string, error) {
		if ctx == nil {
			t.Fatal("context was nil")
		}
		called = true
		return "xoxb-test", nil
	})
	token, err := resolver.ResolveToken(context.Background(), config.WorkspaceProfile{})
	if err != nil {
		t.Fatalf("ResolveToken returned error: %v", err)
	}
	if token != "xoxb-test" {
		t.Fatalf("token = %q, want xoxb-test", token)
	}
	if !called {
		t.Fatal("TokenResolverFunc was not called")
	}
}
