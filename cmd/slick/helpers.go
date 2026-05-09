package main

import (
	"github.com/matcra587/slack-cli/internal/agent"
	climessage "github.com/matcra587/slack-cli/internal/cli/message"
	"github.com/matcra587/slack-cli/internal/config"
	slackgo "github.com/slack-go/slack"
)

// stringPtr returns nil for empty string, otherwise a pointer to the value.
func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// intPtr returns nil for non-positive values, otherwise a pointer to the value.
func intPtr(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}

// resolveAlias resolves a channel/user alias from the workspace profile.
func resolveAlias(profile config.WorkspaceProfile, value string) string {
	return climessage.ResolveAlias(profile, value)
}

// composeBlocks delegates to climessage.ComposeBlocks.
func composeBlocks(content string, raw bool, attribution agent.Attribution) ([]slackgo.Block, error) {
	return climessage.ComposeBlocks(content, raw, attribution)
}

// errString is a simple string-backed error type.
type errString string

func (e errString) Error() string { return string(e) }
