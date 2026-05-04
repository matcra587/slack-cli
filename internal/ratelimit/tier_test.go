package ratelimit_test

import (
	"testing"

	"github.com/matcra587/slack-cli/internal/ratelimit"
)

func TestTierForMethodUsesSlackEndpointLookup(t *testing.T) {
	tests := map[string]ratelimit.Tier{
		"chat.postMessage":        ratelimit.Tier4,
		"chat.update":             ratelimit.Tier4,
		"reactions.add":           ratelimit.Tier4,
		"search.messages":         ratelimit.Tier2,
		"conversations.list":      ratelimit.Tier3,
		"conversations.history":   ratelimit.Tier3,
		"conversations.replies":   ratelimit.Tier3,
		"users.list":              ratelimit.Tier3,
		"auth.test":               ratelimit.Tier2,
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
