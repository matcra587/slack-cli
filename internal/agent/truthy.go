package agent

import "strings"

// TruthyEnv reports whether a string environment-variable value should be
// treated as true. Returns false for empty, "0", "false", "no", "off", and
// "disabled" (case-insensitive, whitespace-trimmed); returns true for
// everything else.
func TruthyEnv(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "false", "no", "off", "disabled":
		return false
	default:
		return true
	}
}
