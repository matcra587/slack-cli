package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/auth"
)

func TestListWorkspacesReturnsMetadataWithoutResolvingCredentials(t *testing.T) {
	dir := isolate(t)
	path := filepath.Join(dir, "profiles.toml")
	data := `schema_version = "1"
default_workspace = "work"
[workspaces.work]
name = "work"
team_id = "TWORK"
team_name = "Work workspace"
token_type = "user"
token_ref = "op://synthetic-private-vault/item/token"
[workspaces.bot]
name = "bot"
team_id = "TBOT"
token_type = "bot"
token_ref = "keychain:synthetic-private-key"
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &credentialStore{getErr: errors.New("credential lookup must not run")}
	result, err := auth.ListWorkspaces(t.Context(), auth.Options{ConfigPath: path, CredentialStore: store, SecretReader: secretReaderFunc(func(context.Context, string) (string, error) { t.Fatal("discovery resolved a secret"); return "", nil })})
	if err != nil || len(result.Workspaces) != 2 || result.DefaultWorkspace != "work" || result.Workspaces[0].Name != "bot" || result.Workspaces[1].TeamName != "Work workspace" {
		t.Fatalf("metadata=%+v error=%v", result, err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "synthetic-private") || strings.Contains(string(raw), "token_ref") {
		t.Fatal("discovery exposed credential references")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != data {
		t.Fatal("discovery modified saved config")
	}
}

func TestListWorkspacesEmptyAndCanceled(t *testing.T) {
	dir := isolate(t)
	result, err := auth.ListWorkspaces(t.Context(), auth.Options{ConfigPath: filepath.Join(dir, "absent.toml")})
	if err != nil || result.Workspaces == nil || len(result.Workspaces) != 0 {
		t.Fatalf("missing config=%+v error=%v", result, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := auth.ListWorkspaces(ctx, auth.Options{ConfigPath: filepath.Join(dir, "absent.toml")}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}
