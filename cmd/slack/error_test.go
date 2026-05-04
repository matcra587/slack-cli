package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestWriteErrorUsesStructuredStderrOnly(t *testing.T) {
	ctx, stdout, stderr := newOutputTestContext(OutputModeJSON)

	exitCode := ctx.WriteError(CLIError{
		Type:     ErrorTypeValidation,
		Message:  "channel is required",
		ExitCode: ExitCodeValidation,
		Details:  map[string]any{"flag": "channel"},
	})

	if exitCode != ExitCodeValidation {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitCodeValidation)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}

	var payload struct {
		Errors []CLIError `json:"errors"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &payload); err != nil {
		t.Fatalf("stderr is not JSON: %v\n%s", err, stderr.String())
	}
	if len(payload.Errors) != 1 {
		t.Fatalf("errors length = %d, want 1", len(payload.Errors))
	}
	if payload.Errors[0].Type != ErrorTypeValidation {
		t.Fatalf("error type = %q", payload.Errors[0].Type)
	}
	if payload.Errors[0].Details["flag"] != "channel" {
		t.Fatalf("error details = %#v", payload.Errors[0].Details)
	}
}

func TestExitCodeConstantsMatchContract(t *testing.T) {
	tests := map[string]int{
		"auth":       ExitCodeAuthFailure,
		"not_found":  ExitCodeNotFound,
		"rate_limit": ExitCodeRateLimit,
		"validation": ExitCodeValidation,
		"server":     ExitCodeServer,
	}

	want := map[string]int{
		"auth":       1,
		"not_found":  2,
		"rate_limit": 3,
		"validation": 4,
		"server":     5,
	}

	for key, got := range tests {
		if got != want[key] {
			t.Fatalf("%s exit code = %d, want %d", key, got, want[key])
		}
	}
}

func TestRateLimitErrorIncludesRetryAfterSecondsWhenSlackSuppliesTiming(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.postMessage": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.SlackResponse{
				Status: http.StatusTooManyRequests,
				Body:   `{"ok":false,"error":"ratelimited"}`,
				Header: http.Header{"Retry-After": []string{"0"}},
			}
		},
	})
	defer server.Close()

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{"message", "send", "--channel", "C123", "--message", "Rate limited"},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want rate limit error")
	}

	var payload struct {
		Errors []CLIError `json:"errors"`
	}
	if unmarshalErr := json.Unmarshal([]byte(stderr), &payload); unmarshalErr != nil {
		t.Fatalf("stderr is not JSON: %v\n%s", unmarshalErr, stderr)
	}
	if len(payload.Errors) != 1 {
		t.Fatalf("errors length = %d, want 1", len(payload.Errors))
	}
	if payload.Errors[0].Type != ErrorTypeRateLimit {
		t.Fatalf("error type = %q, want rate_limit", payload.Errors[0].Type)
	}
	if payload.Errors[0].RetryAfterSeconds == nil || *payload.Errors[0].RetryAfterSeconds != 0 {
		t.Fatalf("retry_after_seconds = %#v, want 0", payload.Errors[0].RetryAfterSeconds)
	}
}
