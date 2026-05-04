package agent_test

import (
	"encoding/json"
	"testing"

	"github.com/matcra587/slack-cli/internal/agent"
)

func TestContextBlockUsesSlackBlockKitShape(t *testing.T) {
	block := agent.ContextBlock(agent.Attribution{
		Enabled: true,
		Label:   "CI/CD pipeline",
		Emoji:   ":gear:",
	})

	raw, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	want := `{"type":"context","elements":[{"type":"mrkdwn","text":":gear: _Sent via slack-cli (CI/CD pipeline)_"}]}`
	if string(raw) != want {
		t.Fatalf("block JSON = %s\nwant       = %s", raw, want)
	}
}

func TestContextBlockReturnsNilWhenAttributionDisabled(t *testing.T) {
	if block := agent.ContextBlock(agent.Attribution{}); block != nil {
		t.Fatalf("ContextBlock disabled = %#v, want nil", block)
	}
}
