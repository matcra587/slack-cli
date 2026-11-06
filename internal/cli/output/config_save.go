package output

import "github.com/matcra587/slack-cli/internal/config"

// ConfigSaveCLIError distinguishes a rejected write from a config replacement
// whose directory sync failed. Callers must keep runtime and credential state
// aligned with the published config before returning the latter error.
func ConfigSaveCLIError(err error, message string) CLIError {
	failure := RuntimeCLIError(message)
	if config.SaveWasPublished(err) {
		failure.Message += "; config was replaced, but crash durability is unconfirmed"
		failure = failure.WithDetails(map[string]any{"config_published": true, "durability_confirmed": false})
	}
	return failure
}
