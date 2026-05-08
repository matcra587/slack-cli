package agent_test

import (
	"testing"

	"github.com/matcra587/slack-cli/internal/agent"
)

func TestTruthyEnv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value string
		want  bool
		note  string
	}{
		// Both prior implementations agreed: false
		{"", false, "empty"},
		{"0", false, "both: zero"},
		{"false", false, "both: false"},
		{"no", false, "both: no"},

		// Both prior implementations agreed: true
		{"1", true, "both: one"},
		{"true", true, "both: true"},
		{"yes", true, "both: yes"},
		{"anything", true, "both: arbitrary truthy"},

		// Diverged: cmd/slick/main.go truthyEnv had "off" → false;
		// internal/agent/detect.go isTruthy did not — merged as false.
		{"off", false, "merged from truthyEnv: off"},

		// Plan-specified additions present in neither prior implementation.
		{"disabled", false, "plan: disabled"},
		{"on", true, "plan: on"},
		{"enabled", true, "plan: enabled"},

		// Case folding (both prior implementations used ToLower).
		{"OFF", false, "case: OFF"},
		{"FALSE", false, "case: FALSE"},
		{"NO", false, "case: NO"},
		{"YES", true, "case: YES"},
		{"True", true, "case: True"},

		// Whitespace trimming (from truthyEnv's TrimSpace; isTruthy lacked this).
		{" 1 ", true, "trim: truthy with spaces"},
		{" off ", false, "trim: off with spaces"},
		{" ", false, "trim: only whitespace"},
	}

	for _, tt := range tests {
		t.Run(tt.note, func(t *testing.T) {
			t.Parallel()
			got := agent.TruthyEnv(tt.value)
			if got != tt.want {
				t.Errorf("TruthyEnv(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
