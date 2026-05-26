package ratelimit

import "strings"

type Tier string

const (
	Tier1       Tier = "tier1"
	Tier2       Tier = "tier2"
	Tier3       Tier = "tier3"
	Tier4       Tier = "tier4"
	TierSpecial Tier = "special"
	TierUnknown Tier = "unknown"
)

// methodTiers maps each Slack Web API method slick actually calls to its
// documented rate-limit tier. Source of truth: https://docs.slack.dev/reference/methods/<name>.
// Keep alphabetised; every entry here should correspond to a slack-go *Context
// callsite under internal/cli/. TestRegistryCoversInvokedMethods enforces that
// nothing slick actually invokes falls through to TierUnknown.
var methodTiers = map[string]Tier{
	"auth.revoke":                  Tier3,
	"auth.test":                    Tier2,
	"chat.delete":                  Tier4,
	"chat.deleteScheduledMessage":  Tier3,
	"chat.getPermalink":            Tier4,
	"chat.postMessage":             TierSpecial,
	"chat.scheduleMessage":         TierSpecial,
	"chat.scheduledMessages.list":  Tier3,
	"chat.update":                  Tier4,
	"conversations.history":        Tier3,
	"conversations.info":           Tier3,
	"conversations.list":           Tier3,
	"conversations.open":           Tier3,
	"conversations.replies":        Tier3,
	"files.completeUploadExternal": Tier4,
	"files.getUploadURLExternal":   Tier4,
	"files.info":                   Tier4,
	"reactions.add":                Tier4,
	"reactions.get":                Tier4,
	"reactions.remove":             Tier4,
	"search.messages":              Tier2,
	"users.getPresence":            Tier3,
	"users.info":                   Tier3,
	"users.list":                   Tier3,
	"users.lookupByEmail":          Tier3,
	"users.profile.set":            Tier3,
}

func TierForMethod(method string) Tier {
	if tier, ok := methodTiers[method]; ok {
		return tier
	}
	if strings.HasPrefix(method, "admin.") {
		return Tier1
	}
	return TierUnknown
}
