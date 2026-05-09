package main

import (
	"github.com/matcra587/slack-cli/internal/agent"
	climessage "github.com/matcra587/slack-cli/internal/cli/message"
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

// composeBlocks delegates to climessage.ComposeBlocks.
func composeBlocks(content string, raw bool, attribution agent.Attribution) ([]slackgo.Block, error) {
	return climessage.ComposeBlocks(content, raw, attribution)
}

// errString is a simple string-backed error type.
type errString string

func (e errString) Error() string { return string(e) }
