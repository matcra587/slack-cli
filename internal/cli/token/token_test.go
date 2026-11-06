package token_test

import (
	"context"
	"testing"

	"github.com/matcra587/slack-cli/internal/cli/token"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/credentials"
)

// These assignments preserve identity rather than only interface compatibility.
// Existing callers can exchange old and new names without conversion.
var (
	_ credentials.EnvTokenResolver        = token.EnvTokenResolver{}
	_ credentials.CredentialTokenResolver = token.CredentialTokenResolver{}
	_ credentials.SecretReader            = token.SecretReaderFunc(nil)
	_ token.SecretReader                  = credentials.SecretReaderFunc(nil)
)

func TestCompatibilityNamesResolveTheSameExplicitCredential(t *testing.T) {
	t.Setenv("SLACK_CLI_TOKEN", "")
	t.Setenv("SLACK_CLI_TOKEN_SYNTHETIC_COMPAT", "xoxp-synthetic-compat")
	value, ok := token.RuntimeEnvToken("synthetic-compat")
	if !ok || value != "xoxp-synthetic-compat" {
		t.Fatal("legacy runtime environment helper lost workspace override")
	}
	value, err := (token.CredentialTokenResolver{}).ResolveToken(context.Background(), config.WorkspaceProfile{Name: "synthetic-compat"})
	if err != nil || value != "xoxp-synthetic-compat" {
		t.Fatalf("legacy resolver failed: %v", err)
	}
}
