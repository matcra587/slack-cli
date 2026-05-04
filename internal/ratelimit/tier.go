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

var methodTiers = map[string]Tier{
	"auth.test":             Tier2,
	"chat.delete":           Tier4,
	"chat.postMessage":      Tier4,
	"chat.update":           Tier4,
	"conversations.history": Tier3,
	"conversations.info":    Tier3,
	"conversations.list":    Tier3,
	"conversations.open":    Tier3,
	"conversations.replies": Tier3,
	"files.upload":          Tier4,
	"reactions.add":         Tier4,
	"reactions.get":         Tier4,
	"reactions.remove":      Tier4,
	"search.messages":       Tier2,
	"users.getPresence":     Tier3,
	"users.info":            Tier3,
	"users.list":            Tier3,
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
