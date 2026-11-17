package slackcatalog_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/slackcatalog"
)

func TestCatalogContainsPublishedContracts(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, verb, required, scope, initial string
		readOnly                             bool
	}{
		{"conversations.history", "GET", "channel", "channels:history", "", true},
		{"chat.postMessage", "POST", "channel", "chat:write", "", false},
		{"canvases.getContent", "POST", "canvas_id", "canvases:read", "", true},
		{"search.messages", "GET", "query", "search:read", "*", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			method, ok := slackcatalog.Lookup(test.name)
			if !ok {
				t.Fatal("published method missing")
			}
			if method.HTTPMethod != test.verb || method.ReadOnly != test.readOnly || method.CursorInitial != test.initial {
				t.Fatalf("wrong request contract: %#v", method)
			}
			if !slices.Contains(method.Required, test.required) {
				t.Fatalf("required arguments %v omit %s", method.Required, test.required)
			}
			foundScope := false
			for _, scopes := range method.Scopes {
				foundScope = foundScope || slices.Contains(scopes, test.scope)
			}
			if !foundScope {
				t.Fatalf("scope %s absent from %#v", test.scope, method.Scopes)
			}
		})
	}
}

func TestCatalogCopiesCannotChangeExecutionPolicy(t *testing.T) {
	first, ok := slackcatalog.Lookup("conversations.history")
	if !ok {
		t.Fatal("missing history method")
	}
	first.Name = "corrupted"
	first.Required[0] = "corrupted"
	first.Arguments[0].Name = "corrupted"
	first.ContentTypes[0] = "corrupted"
	first.Scopes["bot"][0] = "corrupted"
	first.SDKMethods[0] = "corrupted"
	second, _ := slackcatalog.Lookup("conversations.history")
	if second.Name != "conversations.history" || second.Required[0] == "corrupted" || second.Arguments[0].Name == "corrupted" || second.ContentTypes[0] == "corrupted" || second.Scopes["bot"][0] == "corrupted" || second.SDKMethods[0] == "corrupted" {
		t.Fatal("caller changed shared catalog state")
	}
	all := slackcatalog.All()
	all[0].ReadOnly = !all[0].ReadOnly
	lookup, _ := slackcatalog.Lookup(all[0].Name)
	if lookup.ReadOnly == all[0].ReadOnly {
		t.Fatal("All exposes shared policy state")
	}
}

func TestCatalogSnapshotIsCompleteAndOrdered(t *testing.T) {
	t.Parallel()
	metadata := slackcatalog.Provenance()
	methods := slackcatalog.All()
	if metadata.OfficialMethods != 357 || metadata.SDKClientMethods != 514 || metadata.SDKVersion != "v0.36.0" {
		t.Fatalf("unexpected source coverage: %#v", metadata)
	}
	if len(methods) < metadata.OfficialMethods {
		t.Fatal("catalog omits official methods")
	}
	official := 0
	for i, method := range methods {
		if method.Name == "" || len(method.SourceHash) != 64 || method.URL == "" {
			t.Fatalf("missing source provenance: %#v", method)
		}
		if i > 0 && methods[i-1].Name >= method.Name {
			t.Fatalf("duplicate or unordered method: %s", method.Name)
		}
		if method.Source == "official" {
			official++
		} else if method.UnsupportedReason == "" || method.ReadOnly {
			t.Fatalf("unreviewed SDK method is executable as a read: %#v", method)
		}
		if slices.Contains(method.Required, "token") {
			t.Fatalf("token must be supplied through authentication, not parameters: %s", method.Name)
		}
	}
	if official != metadata.OfficialMethods {
		t.Fatalf("official entries = %d, want %d", official, metadata.OfficialMethods)
	}
}

func TestUnknownAndStatefulMethodsAreNotReads(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"files.getUploadURLExternal", "auth.revoke", "conversations.open", "chat.postMessage", "slackLists.download.start", "unknown.get"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			method := slackcatalog.Method{Name: name, HTTPMethod: "GET", Source: "official"}
			slackcatalog.ApplyPolicy(&method)
			if method.ReadOnly {
				t.Fatalf("stateful or unreviewed method %s was classified as a read", name)
			}
		})
	}
}

func TestCredentialAndTransportExceptionsAreExplicit(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"oauth.v2.access", "apps.auth.external.get", "apps.connections.open", "apps.manifest.create", "apps.token.rotate", "rtm.connect"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			method := slackcatalog.Method{Name: name, Source: "official"}
			slackcatalog.ApplyPolicy(&method)
			if !method.SensitiveResponse {
				t.Fatalf("credential-bearing endpoint %s is not marked sensitive", name)
			}
			if (method.Authentication == "configuration" || method.Authentication == "app") && method.UnsupportedReason == "" {
				t.Fatalf("credential exception %s has no unsupported reason", name)
			}
		})
	}
	for _, name := range []string{"oauth.v2.access", "admin.analytics.getFile", "users.setPhoto", "files.upload", "users.setActive", "apps.manifest.create", "apps.manifest.export", "apps.connections.open"} {
		method, ok := slackcatalog.Lookup(name)
		if !ok || strings.TrimSpace(method.UnsupportedReason) == "" {
			t.Errorf("%s has no explicit unsupported-transport/authentication/retirement reason", name)
		}
	}
	for _, method := range slackcatalog.All() {
		if (method.Authentication == "configuration" || method.Authentication == "app") && strings.TrimSpace(method.UnsupportedReason) == "" {
			t.Errorf("%s uses %s authentication without an explicit reason", method.Name, method.Authentication)
		}
	}
	if _, ok := slackcatalog.Lookup("../../auth.test"); ok {
		t.Fatal("path input resolved to a method")
	}
}
