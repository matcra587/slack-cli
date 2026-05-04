package blockkit_test

import (
	"encoding/json"
	"testing"

	"github.com/matcra587/slack-cli/pkg/blockkit"
)

func TestAttributionBlockBuildsContextBlock(t *testing.T) {
	block := blockkit.AttributionBlock(":robot_face:", "agent mode")

	raw, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	want := `{"type":"context","elements":[{"type":"mrkdwn","text":":robot_face: _Sent via slack-cli (agent mode)_"}]}`
	if string(raw) != want {
		t.Fatalf("attribution JSON = %s\nwant             = %s", raw, want)
	}
}
