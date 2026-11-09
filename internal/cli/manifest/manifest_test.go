package manifest_test

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/agent"
	"github.com/matcra587/slack-cli/internal/cli/clitheme"
	climanifest "github.com/matcra587/slack-cli/internal/cli/manifest"
	"github.com/matcra587/slack-cli/internal/cli/runtime/runtimetest"
	"github.com/matcra587/slack-cli/internal/config"
	slackgo "github.com/slack-go/slack"
)

func TestMain(m *testing.M) {
	for _, key := range agent.KnownEnvVars() {
		_ = os.Unsetenv(key)
	}
	os.Exit(m.Run())
}

func TestOrganizationManifestTokenScopes(t *testing.T) {
	for _, tokenType := range []string{"user", "bot"} {
		t.Run(tokenType, func(t *testing.T) {
			out, diagnostics, err := executeTestRoot(t, nil, "http://127.0.0.1:1", "", []string{"manifest", "template", "--preset=organize", "--type=" + tokenType, "--name=Fixture", "--output=human"})
			if err != nil || diagnostics != "" {
				t.Fatalf("manifest failed: %v %s", err, diagnostics)
			}
			var manifest struct {
				OAuth struct {
					Scopes map[string][]string `json:"scopes"`
				} `json:"oauth_config"`
			}
			if json.Unmarshal([]byte(out), &manifest) != nil {
				t.Fatal("manifest is not JSON")
			}
			scopes := manifest.OAuth.Scopes[tokenType]
			for _, scope := range []string{"dnd:read", "pins:read", "pins:write", "bookmarks:read", "bookmarks:write"} {
				if !slices.Contains(scopes, scope) {
					t.Errorf("missing scope %s", scope)
				}
			}
			if slices.Contains(scopes, "dnd:write") != (tokenType == "user") {
				t.Fatal("DND write token eligibility is wrong")
			}
			if slices.Contains(scopes, "chat:write") || slices.Contains(scopes, "reminders:write") {
				t.Fatal("organization preset grants unrelated write permissions")
			}
		})
	}
}

func TestManifestHelpOnlyShowsLocalGenerationCommands(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"manifest", "--help"})
	if err != nil {
		t.Fatalf("manifest --help returned error: %v\nstderr=%s", err, stderr)
	}
	for _, fragment := range []string{
		"template",
		"Output the Slack app manifest",
	} {
		if !strings.Contains(stdout, fragment) {
			t.Fatalf("stdout = %s, want fragment %q", stdout, fragment)
		}
	}
	for _, forbidden := range []string{"create", "update", "delete", "export", "validate", "config-token"} {
		if strings.Contains(stdout, forbidden) {
			t.Fatalf("stdout = %s, did not want app-management command or token fragment %q", stdout, forbidden)
		}
	}
}

func TestManifestTemplateHelpJustifiesDefaultUserScopes(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"manifest", "template", "--help"})
	if err != nil {
		t.Fatalf("manifest template --help returned error: %v\nstderr=%s", err, stderr)
	}
	for _, fragment := range []string{
		"--preset <preset>",
		"Templates",
		"readonly",
		"messaging",
		"files",
		"search",
		"full",
		"Messaging Template User Scopes",
	} {
		if !strings.Contains(stdout, fragment) {
			t.Fatalf("stdout = %s, want fragment %q", stdout, fragment)
		}
	}
	for _, entry := range []struct {
		scope string
		why   string
	}{
		{scope: "channels:history", why: "Read public channel history and thread replies."},
		{scope: "channels:read", why: "List public channels and resolve public channel metadata."},
		{scope: "chat:write", why: "Send, edit, and delete Slack CLI messages."},
		{scope: "groups:history", why: "Read private channel history and thread replies."},
		{scope: "groups:read", why: "List private channels and resolve private channel metadata."},
		{scope: "im:history", why: "Read direct message history and thread replies."},
		{scope: "im:read", why: "List and inspect direct message conversations."},
		{scope: "im:write", why: "Open direct messages and send Slack CLI DMs."},
		{scope: "mpim:history", why: "Read group direct message history and thread replies."},
		{scope: "mpim:read", why: "List and inspect group direct message conversations."},
		{scope: "mpim:write", why: "Open group direct messages when Slack requires a write scope."},
		{scope: "reactions:read", why: "List emoji reactions on messages."},
		{scope: "reactions:write", why: "Add and remove emoji reactions."},
		{scope: "users:read", why: "List users, inspect user metadata, and read presence."},
	} {
		if !strings.Contains(stdout, entry.scope) {
			t.Fatalf("stdout = %s, want scope %q", stdout, entry.scope)
		}
		if !strings.Contains(stdout, entry.why) {
			t.Fatalf("stdout = %s, want justification %q", stdout, entry.why)
		}
	}
	for _, scope := range []string{"files:write", "search:read"} {
		if strings.Contains(stdout, scope+" ") {
			t.Fatalf("stdout = %s, did not want non-default scope justification for %q", stdout, scope)
		}
	}
}

func TestManifestTemplateOutputsImportableSlackCLIManifest(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{
		"manifest", "template", "--name", "example",
	})
	if err != nil {
		t.Fatalf("manifest template returned error: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stdout, `"meta"`) {
		t.Fatalf("stdout = %s, want raw manifest JSON", stdout)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		t.Fatalf("unmarshal raw manifest: %v\nstdout=%s", err, stdout)
	}
	if features, ok := raw["features"].(map[string]any); ok {
		if _, ok := features["bot_user"]; ok {
			t.Fatalf("features.bot_user was present in user manifest: %s", stdout)
		}
	}
	scopes := raw["oauth_config"].(map[string]any)["scopes"].(map[string]any)
	if _, ok := scopes["bot"]; ok {
		t.Fatalf("oauth_config.scopes.bot was present in user manifest: %s", stdout)
	}
	redirectURLs := raw["oauth_config"].(map[string]any)["redirect_urls"].([]any)
	if len(redirectURLs) != 1 {
		t.Fatalf("redirect_urls = %#v, want one default OAuth redirect URL", redirectURLs)
	}
	gotRedirect, ok := redirectURLs[0].(string)
	if !ok {
		t.Fatalf("redirect_urls[0] = %#v, want string", redirectURLs[0])
	}
	if gotRedirect == "http://localhost:0/callback" {
		t.Fatalf("redirect_urls[0] = %q, want generated concrete callback port", gotRedirect)
	}
	if !strings.HasPrefix(gotRedirect, "http://localhost:") || !strings.HasSuffix(gotRedirect, "/callback") {
		t.Fatalf("redirect_urls[0] = %q, want localhost callback URL", gotRedirect)
	}
	if got := raw["oauth_config"].(map[string]any)["pkce_enabled"]; got != true {
		t.Fatalf("oauth_config.pkce_enabled = %#v, want true", got)
	}
	if got := raw["settings"].(map[string]any)["token_rotation_enabled"]; got != true {
		t.Fatalf("settings.token_rotation_enabled = %#v, want true", got)
	}
	var manifest slackgo.Manifest
	if err := json.Unmarshal([]byte(stdout), &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v\nstdout=%s", err, stdout)
	}
	if manifest.Display.Name != "example" {
		t.Fatalf("manifest name = %q, want example", manifest.Display.Name)
	}
	if manifest.Settings.SocketModeEnabled {
		t.Fatal("SocketModeEnabled = true, want false for headless CLI app")
	}
	for _, scope := range []string{"channels:read", "chat:write", "im:write", "reactions:write", "users:read"} {
		if !contains(manifest.OAuthConfig.Scopes.User, scope) {
			t.Fatalf("user scopes = %#v, want %s", manifest.OAuthConfig.Scopes.User, scope)
		}
	}
	for _, scope := range []string{"files:write", "search:read"} {
		if contains(manifest.OAuthConfig.Scopes.User, scope) {
			t.Fatalf("user scopes = %#v, did not want default %s", manifest.OAuthConfig.Scopes.User, scope)
		}
	}
	if len(manifest.OAuthConfig.Scopes.Bot) != 0 {
		t.Fatalf("bot scopes = %#v, want none by default", manifest.OAuthConfig.Scopes.Bot)
	}
	if manifest.Features.BotUser.DisplayName != "" {
		t.Fatalf("bot display name = %q, want no bot user by default", manifest.Features.BotUser.DisplayName)
	}
}

func TestManifestTemplateDefaultRedirectHonorsCallbackPortEnv(t *testing.T) {
	t.Setenv("SLACK_CLI_CALLBACK_PORT", "45678")
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{
		"manifest", "template", "--name", "example",
	})
	if err != nil {
		t.Fatalf("manifest template returned error: %v\nstderr=%s", err, stderr)
	}
	var manifest slackgo.Manifest
	if err := json.Unmarshal([]byte(stdout), &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v\nstdout=%s", err, stdout)
	}
	got := manifest.OAuthConfig.RedirectUrls
	if len(got) != 1 || got[0] != "http://localhost:45678/callback" {
		t.Fatalf("redirect URLs = %#v, want env callback port", got)
	}
}

func TestManifestTemplateCallbackPortOverridesDefaultRedirect(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{
		"manifest", "template", "--name", "example", "--callback-port", "45679",
	})
	if err != nil {
		t.Fatalf("manifest template returned error: %v\nstderr=%s", err, stderr)
	}
	var manifest slackgo.Manifest
	if err := json.Unmarshal([]byte(stdout), &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v\nstdout=%s", err, stdout)
	}
	got := manifest.OAuthConfig.RedirectUrls
	if len(got) != 1 || got[0] != "http://localhost:45679/callback" {
		t.Fatalf("redirect URLs = %#v, want callback-port override", got)
	}
}

func TestManifestTemplatePresetControlsLeastPrivilegeScopes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
		deny []string
	}{
		{
			name: "readonly",
			args: []string{"manifest", "template", "--name", "example", "--preset", "readonly"},
			want: []string{"channels:history", "channels:read", "groups:history", "groups:read", "im:history", "im:read", "mpim:history", "mpim:read", "reactions:read", "users:read"},
			deny: []string{"chat:write", "files:write", "im:write", "mpim:write", "reactions:write", "search:read"},
		},
		{
			name: "files",
			args: []string{"manifest", "template", "--name", "example", "--preset", "files"},
			want: []string{"chat:write", "files:write", "im:write", "mpim:write", "reactions:write"},
			deny: []string{"search:read"},
		},
		{
			name: "search",
			args: []string{"manifest", "template", "--name", "example", "--preset", "search"},
			want: []string{"channels:history", "channels:read", "search:read"},
			deny: []string{"chat:write", "files:write", "im:write", "mpim:write", "reactions:write"},
		},
		{
			name: "full",
			args: []string{"manifest", "template", "--name", "example", "--preset", "full"},
			want: []string{"chat:write", "files:write", "reactions:write", "search:read"},
		},
		{
			name: "override user scopes",
			args: []string{"manifest", "template", "--name", "example", "--preset", "full", "--user-scope", "chat:write"},
			want: []string{"chat:write"},
			deny: []string{"channels:read", "files:write", "search:read"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", tt.args)
			if err != nil {
				t.Fatalf("manifest template returned error: %v\nstderr=%s", err, stderr)
			}
			var manifest slackgo.Manifest
			if err := json.Unmarshal([]byte(stdout), &manifest); err != nil {
				t.Fatalf("unmarshal manifest: %v\nstdout=%s", err, stdout)
			}
			for _, scope := range tt.want {
				if !contains(manifest.OAuthConfig.Scopes.User, scope) {
					t.Fatalf("user scopes = %#v, want %s", manifest.OAuthConfig.Scopes.User, scope)
				}
			}
			for _, scope := range tt.deny {
				if contains(manifest.OAuthConfig.Scopes.User, scope) {
					t.Fatalf("user scopes = %#v, did not want %s", manifest.OAuthConfig.Scopes.User, scope)
				}
			}
		})
	}
}

func TestManifestTemplateTypeControlsScopes(t *testing.T) {
	tests := []struct {
		name     string
		authType string
		wantBot  bool
		wantUser bool
	}{
		{name: "bot", authType: "bot", wantBot: true},
		{name: "user", authType: "user", wantUser: true},
		{name: "both", authType: "both", wantBot: true, wantUser: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{
				"manifest", "template", "--name", "example", "--type", tt.authType,
			})
			if err != nil {
				t.Fatalf("manifest template returned error: %v\nstderr=%s", err, stderr)
			}
			var manifest slackgo.Manifest
			if err := json.Unmarshal([]byte(stdout), &manifest); err != nil {
				t.Fatalf("unmarshal manifest: %v\nstdout=%s", err, stdout)
			}
			if got := len(manifest.OAuthConfig.Scopes.Bot) > 0; got != tt.wantBot {
				t.Fatalf("bot scopes present = %t, want %t: %#v", got, tt.wantBot, manifest.OAuthConfig.Scopes.Bot)
			}
			if got := len(manifest.OAuthConfig.Scopes.User) > 0; got != tt.wantUser {
				t.Fatalf("user scopes present = %t, want %t: %#v", got, tt.wantUser, manifest.OAuthConfig.Scopes.User)
			}
			var raw map[string]any
			if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
				t.Fatalf("unmarshal raw manifest: %v\nstdout=%s", err, stdout)
			}
			scopes := raw["oauth_config"].(map[string]any)["scopes"].(map[string]any)
			_, rawBot := scopes["bot"]
			_, rawUser := scopes["user"]
			if rawBot != tt.wantBot {
				t.Fatalf("raw bot scopes present = %t, want %t\nstdout=%s", rawBot, tt.wantBot, stdout)
			}
			if rawUser != tt.wantUser {
				t.Fatalf("raw user scopes present = %t, want %t\nstdout=%s", rawUser, tt.wantUser, stdout)
			}
			features, _ := raw["features"].(map[string]any)
			_, rawBotUser := features["bot_user"]
			if rawBotUser != tt.wantBot {
				t.Fatalf("raw bot_user present = %t, want %t\nstdout=%s", rawBotUser, tt.wantBot, stdout)
			}
		})
	}
}

func TestManifestSearchScopeIsUserOnlyForBothAuth(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{
		"manifest", "template", "--name", "example", "--type", "both", "--preset", "search",
	})
	if err != nil {
		t.Fatalf("manifest template returned error: %v\nstderr=%s", err, stderr)
	}
	var manifest slackgo.Manifest
	if err := json.Unmarshal([]byte(stdout), &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v\nstdout=%s", err, stdout)
	}
	if !contains(manifest.OAuthConfig.Scopes.User, "search:read") {
		t.Fatalf("user scopes = %#v, want search:read", manifest.OAuthConfig.Scopes.User)
	}
	if contains(manifest.OAuthConfig.Scopes.Bot, "search:read") {
		t.Fatalf("bot scopes = %#v, did not want user-token-only search:read", manifest.OAuthConfig.Scopes.Bot)
	}
}

func TestManifestTemplateYAMLQuotesStringValues(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{
		"manifest", "template", "--name", "example", "--format", "yaml",
	})
	if err != nil {
		t.Fatalf("manifest template yaml returned error: %v\nstderr=%s", err, stderr)
	}
	for _, fragment := range []string{
		`name: "example"`,
		`background_color: "#4A154B"`,
		`pkce_enabled: true`,
		`token_rotation_enabled: true`,
		`- "chat:write"`,
	} {
		if !strings.Contains(stdout, fragment) {
			t.Fatalf("stdout = %s, want fragment %q", stdout, fragment)
		}
	}
}

func TestManifestUnknownAppManagementCommandsFail(t *testing.T) {
	for _, command := range []string{"create", "update", "delete", "export", "validate"} {
		t.Run(command, func(t *testing.T) {
			_, _, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"manifest", command})
			if err == nil {
				t.Fatalf("manifest %s returned nil error", command)
			}
		})
	}
}

func contains(values []string, target string) bool {
	return slices.Contains(values, target)
}

func executeTestRoot(t *testing.T, cfg *config.Config, baseURL, stdin string, args []string) (string, string, error) {
	t.Helper()
	runtime, stdout, stderr := runtimetest.NewRuntime(t, runtimetest.Options{
		Config:       cfg,
		SlackBaseURL: baseURL,
		Stdin:        strings.NewReader(stdin),
		Theme:        clitheme.Default(),
	})
	root := runtimetest.NewRoot(runtime, stdout, stderr)
	root.AddCommand(climanifest.NewCommand(runtime))
	out, diagnostics, err := runtimetest.Run(t, root, args, stdout, stderr)
	if err == nil {
		var frame struct {
			Data struct {
				Manifest json.RawMessage `json:"manifest"`
				Document string          `json:"document"`
			} `json:"data"`
		}
		if json.Unmarshal([]byte(out), &frame) == nil && len(frame.Data.Manifest) > 0 {
			if frame.Data.Document != "" {
				out = frame.Data.Document
			} else {
				out = string(frame.Data.Manifest)
			}
		}
	}
	return out, diagnostics, err
}

func TestTemplateHonorsOutputModes(t *testing.T) {
	for _, format := range []string{"json", "yaml"} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(format+"/"+mode, func(t *testing.T) {
				runtime, stdout, stderr := runtimetest.NewRuntime(t, runtimetest.Options{Theme: clitheme.Default()})
				root := runtimetest.NewRoot(runtime, stdout, stderr)
				root.AddCommand(climanifest.NewCommand(runtime))
				out, diagnostics, err := runtimetest.Run(t, root, []string{"manifest", "template", "--format", format, "--output", mode}, stdout, stderr)
				if err != nil || diagnostics != "" {
					t.Fatalf("err=%v stderr=%s", err, diagnostics)
				}
				if mode == "human" && format == "yaml" {
					if !strings.Contains(out, "display_information:") || strings.Contains(out, `"meta"`) {
						t.Fatalf("not importable YAML: %s", out)
					}
					return
				}
				var data map[string]json.RawMessage
				if err := json.Unmarshal([]byte(out), &data); err != nil {
					t.Fatal(err)
				}
				if mode == "human" {
					if _, ok := data["display_information"]; !ok {
						t.Fatal("human mode lost raw manifest")
					}
					return
				}
				if mode == "json" {
					if len(data) != 4 || string(data["ok"]) != "true" {
						t.Fatalf("invalid result frame: %s", out)
					}
					if err := json.Unmarshal(data["data"], &data); err != nil {
						t.Fatal(err)
					}
				}
				if len(data["manifest"]) == 0 || string(data["format"]) != `"`+format+`"` {
					t.Fatalf("missing importable artifact: %s", out)
				}
				if format == "yaml" && len(data["document"]) == 0 {
					t.Fatal("YAML document missing")
				}
			})
		}
	}
}
