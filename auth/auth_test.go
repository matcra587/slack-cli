package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/auth"
)

const syntheticToken = "xoxp-synthetic-auth-fixture"

func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SLICK_CONFIG", filepath.Join(dir, "missing.toml"))
	t.Setenv("SLACK_CLI_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("SLACK_CLI_DEFAULT_WORKSPACE", "")
	t.Setenv("SLACK_CLI_TOKEN", "")
	t.Setenv("SLACK_CLI_TOKEN_ONE", "")
	t.Setenv("SLACK_CLI_TOKEN_TWO", "")
	t.Setenv("SLICK_AUTH_TEST_TOKEN", syntheticToken)
	return dir
}

func writeConfig(t *testing.T, path, ref string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := `schema_version = "1"
default_workspace = "one"
[workspaces.one]
name = "one"
team_id = "TONE"
team_name = "Synthetic One"
token_type = "user"
token_ref = ` + fmt.Sprintf("%q", ref) + `
[workspaces.two]
name = "two"
team_id = "TTWO"
token_type = "bot"
token_ref = "keychain:slack-cli/two"
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
}

type credentialStore struct {
	values map[string]string
	getErr error
	setErr error
	reads  []string
	writes int
}

func (s *credentialStore) Get(_, user string) (string, error) {
	s.reads = append(s.reads, user)
	if s.getErr != nil {
		return "", s.getErr
	}
	value, ok := s.values[user]
	if !ok {
		return "", errors.New("synthetic credential missing")
	}
	return value, nil
}

func (s *credentialStore) Set(_, user, value string) error {
	if s.setErr != nil {
		return s.setErr
	}
	s.writes++
	s.values[user] = value
	return nil
}

func (*credentialStore) Delete(_, _ string) error { return errors.New("unexpected delete") }

func TestResolveUsesSelectedWorkspaceAndFreshCredentials(t *testing.T) {
	dir := isolate(t)
	path := filepath.Join(dir, "config.toml")
	writeConfig(t, path, "keychain:slack-cli/one")
	store := &credentialStore{values: map[string]string{
		"one": `{"access_token":"xoxp-synthetic-one"}`,
		"two": `{"access_token":"xoxb-synthetic-two"}`,
	}}
	opts := auth.Options{ConfigPath: path, CredentialStore: store}
	first, err := auth.Resolve(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if first.Workspace != "one" || first.Token() != "xoxp-synthetic-one" || first.TeamID != "TONE" {
		t.Fatalf("unexpected default identity: %v", first)
	}
	opts.Workspace = "TWO"
	second, err := auth.Resolve(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if second.Workspace != "two" || second.Token() != "xoxb-synthetic-two" || second.TokenType != "bot" {
		t.Fatalf("unexpected explicit identity: %v", second)
	}
	store.values["two"] = `{"access_token":"xoxb-synthetic-rotated"}`
	changed, err := auth.Resolve(t.Context(), opts)
	if err != nil || changed.Token() != "xoxb-synthetic-rotated" {
		t.Fatalf("credential update was not observed: %v", err)
	}
	opts.Workspace = ""
	restored, err := auth.Resolve(t.Context(), opts)
	if err != nil || restored.Workspace != "one" {
		t.Fatalf("removing selection did not restore default: %v", err)
	}
	if strings.Join(store.reads, ",") != "one,two,two,one" || store.writes != 0 {
		t.Fatalf("unexpected credential access: reads=%v writes=%d", store.reads, store.writes)
	}
}

func TestResolveConfigPathPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name     string
		selected int
	}{
		{"explicit", 0}, {"slick environment", 1}, {"legacy environment", 2}, {"XDG", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := isolate(t)
			paths := []string{filepath.Join(dir, "explicit.toml"), filepath.Join(dir, "slick.toml"), filepath.Join(dir, "legacy.toml"), filepath.Join(dir, "slick", "config.toml")}
			for i, path := range paths {
				writeConfig(t, path, "env:SLICK_AUTH_TEST_TOKEN")
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				raw = []byte(strings.ReplaceAll(string(raw), `team_id = "TONE"`, fmt.Sprintf(`team_id = "TFIXTURE%d"`, i)))
				if err := os.WriteFile(path, raw, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			opts := auth.Options{}
			if tc.selected == 0 {
				opts.ConfigPath = paths[0]
			}
			if tc.selected <= 1 {
				t.Setenv("SLICK_CONFIG", paths[1])
			} else {
				t.Setenv("SLICK_CONFIG", "")
			}
			if tc.selected <= 2 {
				t.Setenv("SLACK_CLI_CONFIG", paths[2])
			}
			got, err := auth.Resolve(t.Context(), opts)
			if err != nil || got.Token() != syntheticToken || got.TeamID != fmt.Sprintf("TFIXTURE%d", tc.selected) {
				t.Fatalf("wrong config selected: session=%v err=%v", got, err)
			}
		})
	}
}

func TestResolveSessionRedactsCredentials(t *testing.T) {
	dir := isolate(t)
	path := filepath.Join(dir, "config.toml")
	writeConfig(t, path, "keychain:slack-cli/one")
	store := &credentialStore{values: map[string]string{"one": `{"access_token":"xoxp-synthetic-private","refresh_token":"synthetic-refresh-private","client_id":"synthetic-client-private"}`}}
	session, err := auth.Resolve(t.Context(), auth.Options{ConfigPath: path, CredentialStore: store})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	for name, output := range map[string]string{"String": session.String(), "value": fmt.Sprintf("%v", session), "expanded": fmt.Sprintf("%+v", session), "Go": fmt.Sprintf("%#v", session), "pointer": fmt.Sprintf("%#v", &session), "JSON": string(raw)} {
		for _, secret := range []string{"xoxp-synthetic-private", "synthetic-refresh-private", "synthetic-client-private", "access_token", "refresh_token"} {
			if strings.Contains(output, secret) {
				t.Errorf("%s leaked credential material", name)
			}
		}
	}
	if session.Token() != "xoxp-synthetic-private" {
		t.Fatal("explicit token accessor did not return selected credential")
	}
}

func TestResolveFailuresAreSafeAndPreserveState(t *testing.T) {
	for _, tc := range []struct {
		name, ref, value string
		missingConfig    bool
		workspace        string
	}{
		{name: "missing config", missingConfig: true},
		{name: "missing workspace", ref: "env:SLICK_AUTH_TEST_TOKEN", workspace: "missing"},
		{name: "unsupported reference", ref: "xoxp-synthetic-raw"},
		{name: "missing environment", ref: "env:SLICK_AUTH_TEST_MISSING"},
		{name: "missing stored token", ref: "keychain:slack-cli/one"},
		{name: "malformed credential", ref: "keychain:slack-cli/one", value: `{"access_token":`},
		{name: "empty credential", ref: "keychain:slack-cli/one", value: `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := isolate(t)
			t.Setenv("SLICK_AUTH_TEST_MISSING", "")
			path := filepath.Join(dir, "config.toml")
			store := &credentialStore{values: map[string]string{}}
			if tc.value != "" {
				store.values["one"] = tc.value
			}
			if !tc.missingConfig {
				writeConfig(t, path, tc.ref)
			}
			prior, _ := os.ReadFile(path)
			session, err := auth.Resolve(t.Context(), auth.Options{ConfigPath: path, Workspace: tc.workspace, CredentialStore: store})
			if !errors.Is(err, auth.ErrNotAuthenticated) || session.Token() != "" {
				t.Fatalf("failure did not reject credential resolution: session=%v err=%v", session, err)
			}
			after, _ := os.ReadFile(path)
			if string(after) != string(prior) || store.writes != 0 {
				t.Fatal("failure changed prior configuration or credential state")
			}
		})
	}
}

func TestResolveProviderErrorRedactionAndCause(t *testing.T) {
	dir := isolate(t)
	path := filepath.Join(dir, "config.toml")
	writeConfig(t, path, "keychain:slack-cli/one")
	cause := errors.New("provider included xoxp-synthetic-private in its diagnostic")
	_, err := auth.Resolve(t.Context(), auth.Options{ConfigPath: path, CredentialStore: &credentialStore{getErr: cause}})
	if !errors.Is(err, cause) || !errors.Is(err, auth.ErrNotAuthenticated) {
		t.Fatal("credential failure lost its classifications")
	}
	for _, output := range []string{fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
		if strings.Contains(output, "xoxp-synthetic-private") {
			t.Fatal("provider diagnostic exposed in normal formatting")
		}
	}
}

type secretReaderFunc func(context.Context, string) (string, error)

func (f secretReaderFunc) Read(ctx context.Context, ref string) (string, error) { return f(ctx, ref) }

func TestResolveExplicitSecretReferenceUsesInjectedReader(t *testing.T) {
	dir := isolate(t)
	path := filepath.Join(dir, "config.toml")
	writeConfig(t, path, "op://Synthetic/fixture/access")
	reader := secretReaderFunc(func(ctx context.Context, ref string) (string, error) {
		if ctx.Err() != nil || ref != "op://Synthetic/fixture/access" {
			t.Fatal("unexpected secret read")
		}
		return `{"access_token":"xoxp-synthetic-secret-manager"}`, nil
	})
	session, err := auth.Resolve(t.Context(), auth.Options{ConfigPath: path, SecretReader: reader})
	if err != nil || session.Token() != "xoxp-synthetic-secret-manager" {
		t.Fatalf("secret resolution failed: %v", err)
	}
}

func TestResolveHonorsCancellation(t *testing.T) {
	dir := isolate(t)
	path := filepath.Join(dir, "config.toml")
	writeConfig(t, path, "op://Synthetic/fixture/access")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := auth.Resolve(ctx, auth.Options{ConfigPath: path, SecretReader: secretReaderFunc(func(context.Context, string) (string, error) {
		t.Fatal("canceled request read a secret")
		return "", nil
	})})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation", err)
	}
}

func TestResolveEnvironmentOverrideAvoidsCredentialStore(t *testing.T) {
	dir := isolate(t)
	path := filepath.Join(dir, "config.toml")
	writeConfig(t, path, "keychain:slack-cli/one")
	t.Setenv("SLACK_CLI_TOKEN", "xoxp-synthetic-global")
	t.Setenv("SLACK_CLI_TOKEN_ONE", "xoxp-synthetic-workspace")
	store := &credentialStore{getErr: errors.New("keychain must not be opened")}
	opts := auth.Options{ConfigPath: path, CredentialStore: store}
	session, err := auth.Resolve(t.Context(), opts)
	if err != nil || session.Token() != "xoxp-synthetic-workspace" {
		t.Fatalf("workspace override failed: %v", err)
	}
	t.Setenv("SLACK_CLI_TOKEN_ONE", "")
	session, err = auth.Resolve(t.Context(), opts)
	if err != nil || session.Token() != "xoxp-synthetic-global" || len(store.reads) != 0 {
		t.Fatalf("global override failed: %v", err)
	}
}

func TestResolveCredentialDisappearanceDoesNotReturnCachedToken(t *testing.T) {
	dir := isolate(t)
	path := filepath.Join(dir, "config.toml")
	writeConfig(t, path, "keychain:slack-cli/one")
	store := &credentialStore{values: map[string]string{"one": `{"access_token":"xoxp-synthetic-old"}`}}
	opts := auth.Options{ConfigPath: path, CredentialStore: store}
	if _, err := auth.Resolve(t.Context(), opts); err != nil {
		t.Fatal(err)
	}
	delete(store.values, "one")
	session, err := auth.Resolve(t.Context(), opts)
	if !errors.Is(err, auth.ErrNotAuthenticated) || session.Token() != "" {
		t.Fatalf("disappeared credential was reused: %v", err)
	}
}

func TestResolveSecretReaderHonorsContextDeadline(t *testing.T) {
	dir := isolate(t)
	path := filepath.Join(dir, "config.toml")
	writeConfig(t, path, "op://Synthetic/fixture/access")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	reader := secretReaderFunc(func(ctx context.Context, _ string) (string, error) { <-ctx.Done(); return "", ctx.Err() })
	_, err := auth.Resolve(ctx, auth.Options{ConfigPath: path, SecretReader: reader})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline", err)
	}
}
