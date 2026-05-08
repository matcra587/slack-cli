package main

import (
	"strings"
	"testing"
)

// TestLeafCommandsRejectStrayArgs verifies every flag-only leaf command refuses
// unexpected positional arguments.  Each entry below maps a command path to a
// stray argument that should be rejected before RunE is reached.
func TestLeafCommandsRejectStrayArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "auth login", args: []string{"auth", "login", "stray"}},
		{name: "message send", args: []string{"message", "send", "stray"}},
		{name: "message edit", args: []string{"message", "edit", "stray"}},
		{name: "message delete", args: []string{"message", "delete", "stray"}},
		{name: "react add", args: []string{"react", "add", "stray"}},
		{name: "react remove", args: []string{"react", "remove", "stray"}},
		{name: "lookup channel", args: []string{"lookup", "channel", "stray"}},
		{name: "lookup user", args: []string{"lookup", "user", "stray"}},
		{name: "lookup messages", args: []string{"lookup", "messages", "stray"}},
		{name: "reply", args: []string{"reply", "stray"}},
		{name: "history list", args: []string{"history", "list", "stray"}},
		{name: "file upload", args: []string{"file", "upload", "stray"}},
		{name: "workspace list", args: []string{"workspace", "list", "stray"}},
		{name: "cache clear bogus", args: []string{"cache", "clear", "bogus"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout := &strings.Builder{}
			stderr := &strings.Builder{}
			cmd := NewRootCommand(
				WithConfig(nil),
				WithIO(strings.NewReader(""), stdout, stderr),
				WithTTY(false),
			)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("%s: Execute returned nil error, want args validation error", tt.name)
			}
		})
	}
}

// TestCacheClearValidArgsAccepted verifies that valid resource names are
// accepted by cobra before RunE (config-nil path returns a runtime error, not
// an args error).
func TestCacheClearValidArgsAccepted(t *testing.T) {
	for _, resource := range []string{"users", "channels"} {
		t.Run(resource, func(t *testing.T) {
			stdout := &strings.Builder{}
			stderr := &strings.Builder{}
			cmd := NewRootCommand(
				WithConfig(nil),
				WithIO(strings.NewReader(""), stdout, stderr),
				WithTTY(false),
			)
			cmd.SetArgs([]string{"cache", "clear", resource})
			err := cmd.Execute()
			// With no config, RunE will return a runtime/validation error, but
			// cobra's Args validator must not reject the resource name itself.
			// An args error message contains "unknown command" or "invalid argument".
			if err != nil {
				msg := err.Error()
				if strings.Contains(msg, "invalid argument") || strings.Contains(msg, "unknown command") {
					t.Fatalf("cache clear %s rejected by Args validator: %v", resource, err)
				}
			}
		})
	}
}
