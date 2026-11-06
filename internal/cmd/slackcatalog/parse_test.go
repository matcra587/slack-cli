package main

import (
	"strings"
	"testing"
)

const historySource = `---
title: "conversations.history API method"
summary: "Fetches a conversation's history of messages and events."
url: "https://docs.slack.dev/reference/methods/conversations.history"
type: "reference"
subtype: "method"
method_name: "conversations.history"
http_method: "GET"
rate_limit: "t3"
scopes:
  bot: ["channels:history", "groups:history"]
  user: ["channels:history", "groups:history"]
---

## Facts
` + "**Content types**\n\n`application/x-www-form-urlencoded`\n\n**Rate Limits**Tier 3\n" + `
## Arguments
### Required arguments
` + "**`token`**`string`Required\n\n**`channel`**`string`Required\n" + `
### Optional arguments
` + "**`cursor`**`string`Optional\n\n**`limit`**`number`Optional\n\n_Default:_ `100`\n" + `
## Usage info
Published description.
## Errors
` + "**`not_a_parameter`**`string`Required\n"

func TestParsePublishedMethodContract(t *testing.T) {
	t.Parallel()
	method, err := parseMethod([]byte(historySource))
	if err != nil {
		t.Fatal(err)
	}
	if method.Name != "conversations.history" || method.HTTPMethod != "GET" || method.RateLimit != "t3" || !method.ReadOnly || method.Pagination != "cursor" {
		t.Fatalf("unexpected method: %#v", method)
	}
	if len(method.Arguments) != 3 || len(method.Required) != 1 || method.Required[0] != "channel" || method.Arguments[2].Default != "100" {
		t.Fatalf("incorrect argument contract: %#v", method)
	}
	if len(method.Scopes["bot"]) != 2 || len(method.ContentTypes) != 1 || method.Authentication != "bearer" {
		t.Fatalf("incorrect authorization/transport contract: %#v", method)
	}
}

func TestParseMethodRejectsBrokenOrUntrustedMetadata(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, source string }{
		{"missing frontmatter", "# Endpoint"},
		{"unterminated frontmatter", "---\nmethod_name: \"auth.test\""},
		{"invalid scalar", strings.Replace(historySource, `rate_limit: "t3"`, `rate_limit: {object: true}`, 1)},
		{"invalid scopes", strings.Replace(historySource, `["channels:history", "groups:history"]`, `false`, 1)},
		{"mismatched canonical URL", strings.Replace(historySource, `https://docs.slack.dev/reference/methods/conversations.history`, `https://example.com/endpoint`, 1)},
		{"unsupported verb", strings.Replace(historySource, `http_method: "GET"`, `http_method: "DELETE"`, 1)},
		{"path in method name", strings.ReplaceAll(historySource, "conversations.history", "../../auth.test")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := parseMethod([]byte(test.source)); err == nil {
				t.Fatal("malformed source was accepted")
			}
		})
	}
}

func TestParseMethodUsesPublishedBodyVerbWhenFrontmatterOmitsIt(t *testing.T) {
	t.Parallel()
	source := strings.Replace(historySource, "http_method: \"GET\"\n", "", 1)
	source += "\nGET https://slack.com/api/conversations.history\n"
	method, err := parseMethod([]byte(source))
	if err != nil || method.HTTPMethod != "GET" {
		t.Fatalf("method = %#v, error = %v", method, err)
	}
}

func TestMethodIndexAcceptsOnlyOfficialMethodURLs(t *testing.T) {
	t.Parallel()
	urls := methodURLs("https://docs.slack.dev/reference/methods/auth.test.md\nhttps://docs.slack.dev/reference/methods/auth.test.md\nhttps://example.com/reference/methods/auth.revoke.md\nhttps://docs.slack.dev/reference/methods/../../private.md")
	if len(urls) != 1 || urls[0] != "https://docs.slack.dev/reference/methods/auth.test.md" {
		t.Fatalf("URLs = %v", urls)
	}
}
