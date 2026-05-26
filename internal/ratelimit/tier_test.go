package ratelimit_test

import (
	"testing"

	"github.com/matcra587/slack-cli/internal/ratelimit"
)

func TestTierForMethodUsesSlackEndpointLookup(t *testing.T) {
	tests := map[string]ratelimit.Tier{
		"auth.revoke":                  ratelimit.Tier3,
		"auth.test":                    ratelimit.Tier2,
		"chat.delete":                  ratelimit.Tier4,
		"chat.deleteScheduledMessage":  ratelimit.Tier3,
		"chat.getPermalink":            ratelimit.Tier4,
		"chat.postMessage":             ratelimit.TierSpecial,
		"chat.scheduleMessage":         ratelimit.TierSpecial,
		"chat.scheduledMessages.list":  ratelimit.Tier3,
		"chat.update":                  ratelimit.Tier4,
		"conversations.history":        ratelimit.Tier3,
		"conversations.list":           ratelimit.Tier3,
		"conversations.replies":        ratelimit.Tier3,
		"files.completeUploadExternal": ratelimit.Tier4,
		"files.getUploadURLExternal":   ratelimit.Tier4,
		"files.info":                   ratelimit.Tier4,
		"reactions.add":                ratelimit.Tier4,
		"search.messages":              ratelimit.Tier2,
		"users.list":                   ratelimit.Tier3,
		"users.lookupByEmail":          ratelimit.Tier3,
		"users.profile.set":            ratelimit.Tier3,
		// Admin-namespace catchall: any unlisted admin.* method maps to Tier1.
		"admin.conversations.foo": ratelimit.Tier1,
		"unknown.method":          ratelimit.TierUnknown,
	}

	for method, want := range tests {
		t.Run(method, func(t *testing.T) {
			if got := ratelimit.TierForMethod(method); got != want {
				t.Fatalf("TierForMethod(%q) = %q, want %q", method, got, want)
			}
		})
	}
}

// TestRegistryCoversInvokedMethods is a drift fence: every Slack Web API method
// slick actually calls (via a slack-go *Context wrapper under internal/cli/)
// must be registered with a real tier. A new method that lands here but not in
// methodTiers will throttle naively under TierUnknown, so this test demands an
// explicit tier-vs-callsite update whenever the surface grows.
//
// When you add a new slack-go callsite under internal/cli/, add the method
// here AND in methodTiers (source: https://docs.slack.dev/reference/methods/<name>).
func TestRegistryCoversInvokedMethods(t *testing.T) {
	invoked := []string{
		"auth.revoke",
		"auth.test",
		"chat.delete",
		"chat.deleteScheduledMessage",
		"chat.getPermalink",
		"chat.postMessage",
		"chat.scheduleMessage",
		"chat.scheduledMessages.list",
		"chat.update",
		"conversations.history",
		"conversations.info",
		"conversations.list",
		"conversations.open",
		"conversations.replies",
		"files.completeUploadExternal",
		"files.getUploadURLExternal",
		"files.info",
		"reactions.add",
		"reactions.get",
		"reactions.remove",
		"search.messages",
		"users.getPresence",
		"users.info",
		"users.list",
		"users.lookupByEmail",
		"users.profile.set",
	}
	for _, method := range invoked {
		if got := ratelimit.TierForMethod(method); got == ratelimit.TierUnknown {
			t.Errorf("TierForMethod(%q) = TierUnknown — add it to methodTiers with its canonical Slack tier", method)
		}
	}
}
