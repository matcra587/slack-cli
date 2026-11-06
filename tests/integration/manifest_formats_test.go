//go:build integration

package integration_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestBinaryManifestFormatsPreservePresetScopes(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, preset := range []string{"readonly", "messaging", "files", "search", "full"} {
		for _, kind := range []string{"user", "bot", "both"} {
			for _, format := range []string{"json", "yaml", "yml"} {
				for _, mode := range []string{"json", "compact", "human"} {
					t.Run(preset+"/"+kind+"/"+format+"/"+mode, func(t *testing.T) {
						run := manifestFixture(t, binary)
						out, diagnostic, code := run("53221", "--preset="+preset, "--type="+kind, "--format="+format, "--output="+mode)
						if code != 0 || diagnostic != "" {
							t.Fatal("valid manifest preset/type/format failed")
						}
						manifest := decodeManifestFormat(t, out, format, mode)
						want := publishedManifestScopes(preset)
						var bot, user []string
						if kind == "user" || kind == "both" {
							user = want
						}
						if kind == "bot" || kind == "both" {
							for _, scope := range want {
								if scope != "search:read" && scope != "users.profile:write" {
									bot = append(bot, scope)
								}
							}
						}
						assertManifestScopes(t, manifest, bot, user)
					})
				}
			}
		}
	}
}

func TestBinaryManifestScopeOverridesRemainEquivalentAcrossFormats(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name      string
		args      []string
		bot, user []string
	}{
		{"user override", []string{"--type=user", "--preset=full", "--user-scope= users:read ", "--user-scope=chat:write", "--user-scope=chat:write"}, nil, []string{"chat:write", "users:read"}},
		{"bot override", []string{"--type=bot", "--preset=full", "--bot-scope=chat:write", "--bot-scope=search:read", "--bot-scope=users.profile:write"}, []string{"chat:write"}, nil},
		{"empty effective scopes", []string{"--type=bot", "--bot-scope=search:read"}, nil, nil},
		{"both overrides", []string{"--type=both", "--preset=full", "--bot-scope=reactions:read", "--user-scope=search:read", "--user-scope=users.profile:write"}, []string{"reactions:read"}, []string{"search:read", "users.profile:write"}},
		{"user shape ignores bot override", []string{"--type=user", "--bot-scope=files:write"}, nil, publishedManifestScopes("messaging")},
		{"bot shape ignores user override", []string{"--type=bot", "--user-scope=users.profile:write"}, publishedManifestScopes("messaging"), nil},
		{"blank override restores preset", []string{"--type=user", "--preset=readonly", "--user-scope= "}, nil, publishedManifestScopes("readonly")},
		{"preset compatibility alias", []string{"--type=user", "--template=readonly"}, nil, publishedManifestScopes("readonly")},
	} {
		for _, format := range []string{"json", "yaml"} {
			for _, mode := range []string{"json", "compact", "human"} {
				t.Run(tc.name+"/"+format+"/"+mode, func(t *testing.T) {
					run := manifestFixture(t, binary)
					args := append([]string{}, tc.args...)
					args = append(args, "--format="+format, "--output="+mode)
					out, diagnostic, code := run("53221", args...)
					if code != 0 || diagnostic != "" {
						t.Fatal("manifest scope override failed")
					}
					assertManifestScopes(t, decodeManifestFormat(t, out, format, mode), tc.bot, tc.user)
					out, diagnostic, code = run("53221", "--format="+format, "--output="+mode)
					if code != 0 || diagnostic != "" {
						t.Fatal("manifest failed after scope overrides were removed")
					}
					assertManifestScopes(t, decodeManifestFormat(t, out, format, mode), nil, publishedManifestScopes("messaging"))
				})
			}
		}
	}
}

func TestBinaryManifestRejectsUnknownPresetTypeAndFormat(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, flag := range []string{"preset", "type", "format"} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(flag+"/"+mode, func(t *testing.T) {
				run := manifestFixture(t, binary)
				out, diagnostic, code := run("53221", "--"+flag+"=invalid", "--output="+mode)
				if code != 4 {
					t.Fatal("unknown manifest option emitted success")
				}
				if mode == "human" {
					if out != "" || diagnostic == "" {
						t.Fatal("invalid manifest option used wrong human streams")
					}
				} else {
					var frame map[string]json.RawMessage
					if json.Unmarshal([]byte(out), &frame) != nil || len(frame) != 4 || string(frame["ok"]) != "false" || string(frame["data"]) != "null" || diagnostic != "" {
						t.Fatal("invalid manifest option lost common failure envelope")
					}
				}
			})
		}
	}
}

func TestBinaryManifestYAMLRetainsQuotedDisplayStrings(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, mode := range []string{"json", "compact", "human"} {
		t.Run(mode, func(t *testing.T) {
			run := manifestFixture(t, binary)
			name := `Release "candidate": é`
			description := "Line one\nLine \"two\" # approved"
			long := strings.TrimSpace(strings.Repeat("Review é: ready. ", 20))
			out, diagnostic, code := run("53221", "--name="+name, "--description="+description, "--long-description="+long, "--format=yaml", "--output="+mode)
			if code != 0 || diagnostic != "" {
				t.Fatal("valid quoted manifest text failed")
			}
			doc := decodeManifestFormat(t, out, "yaml", mode)
			display, ok := doc["display_information"].(map[string]any)
			if !ok || display["name"] != name || display["description"] != description || display["long_description"] != long {
				t.Fatal("YAML changed quoted, multiline or Unicode display content")
			}
		})
	}
}

func decodeManifestFormat(t *testing.T, out, format, mode string) map[string]any {
	t.Helper()
	decodeYAML := func(text string) map[string]any {
		t.Helper()
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
			t.Fatalf("manifest is not valid YAML: %v", err)
		}
		return doc
	}
	if mode == "human" && format != "json" {
		return decodeYAML(out)
	}
	var doc map[string]any
	if json.Unmarshal([]byte(out), &doc) != nil {
		t.Fatal("manifest is not valid JSON")
	}
	if mode == "json" {
		if len(doc) != 4 || doc["ok"] != true || doc["errors"] == nil {
			t.Fatal("manifest lost common success envelope")
		}
		var ok bool
		doc, ok = doc["data"].(map[string]any)
		if !ok {
			t.Fatal("manifest lost result payload")
		}
	}
	if mode == "human" {
		return doc
	}
	wantFormat := "json"
	if format != "json" {
		wantFormat = "yaml"
	}
	if doc["format"] != wantFormat {
		t.Fatal("manifest format alias was not normalized")
	}
	manifest, ok := doc["manifest"].(map[string]any)
	if !ok {
		t.Fatal("manifest payload has no manifest object")
	}
	if format != "json" {
		document, ok := doc["document"].(string)
		if !ok || !reflect.DeepEqual(decodeYAML(document), manifest) {
			t.Fatal("YAML manifest differs from machine manifest object")
		}
	}
	return manifest
}

func assertManifestScopes(t *testing.T, manifest map[string]any, wantBot, wantUser []string) {
	t.Helper()
	oauth, ok := manifest["oauth_config"].(map[string]any)
	if !ok {
		t.Fatal("manifest lost OAuth configuration")
	}
	scopes, ok := oauth["scopes"].(map[string]any)
	if !ok {
		t.Fatal("manifest scopes is not an object")
	}
	for _, tc := range []struct {
		key  string
		want []string
	}{{"bot", wantBot}, {"user", wantUser}} {
		values, exists := scopes[tc.key]
		if len(tc.want) == 0 {
			if exists {
				t.Fatal("manifest emitted scopes for an unused token shape")
			}
			continue
		}
		list, ok := values.([]any)
		if !ok {
			t.Fatal("manifest scopes is not an array")
		}
		got := make([]string, 0, len(list))
		for _, value := range list {
			scope, ok := value.(string)
			if !ok {
				t.Fatal("manifest scope is not a string")
			}
			got = append(got, scope)
		}
		want := slices.Clone(tc.want)
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Fatalf("manifest %s scopes differ: got=%v want=%v", tc.key, got, want)
		}
	}
	features, _ := manifest["features"].(map[string]any)
	_, hasBot := features["bot_user"]
	if hasBot != (len(wantBot) != 0) || oauth["pkce_enabled"] != true {
		t.Fatal("manifest lost matching bot shape or PKCE")
	}
	settings, ok := manifest["settings"].(map[string]any)
	if !ok || settings["token_rotation_enabled"] != true {
		t.Fatal("manifest lost token rotation")
	}
}

func publishedManifestScopes(preset string) []string {
	readonly := []string{"channels:history", "channels:read", "groups:history", "groups:read", "im:history", "im:read", "mpim:history", "mpim:read", "reactions:read", "users:read"}
	messaging := append(slices.Clone(readonly), "chat:write", "im:write", "mpim:write", "reactions:write", "users:read.email")
	switch preset {
	case "readonly":
		return readonly
	case "messaging":
		return messaging
	case "files":
		return append(messaging, "files:write")
	case "search":
		return append(readonly, "search:read")
	case "full":
		return append(messaging, "files:write", "search:read", "users.profile:write")
	default:
		panic("unknown test preset")
	}
}
