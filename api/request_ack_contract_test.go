package api_test

import (
	"errors"
	"io"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/matcra587/slack-cli/api"
)

func TestCallRejectsContradictoryAcknowledgementWithoutReplacingCallerState(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	client, _ := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/chat.delete" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer "+fixtureToken {
			t.Error("mutation request changed its method, target or synthetic authentication")
		}
		w.Header().Set("Content-Type", "application/json")
		body := `{"ok":true,"error":"missing_scope","marker":"unconfirmed"}`
		if calls.Add(1) > 1 {
			body = `{"ok":true,"marker":"confirmed"}`
		}
		if _, err := io.WriteString(w, body); err != nil {
			t.Error(err)
		}
	})
	params := map[string]any{"channel": "C_SYNTHETIC", "ts": "1746284582.123456"}
	result := map[string]any{"marker": "prior"}
	err := client.CallWithContext(t.Context(), "chat.delete", params, &result)
	var failure *api.Error
	if !errors.As(err, &failure) || failure.Code != "invalid_response" || failure.StatusCode != http.StatusOK {
		t.Fatalf("contradictory mutation acknowledgement did not fail as invalid_response: %v", err)
	}
	if calls.Load() != 1 || !reflect.DeepEqual(result, map[string]any{"marker": "prior"}) {
		t.Fatal("invalid acknowledgement replayed the mutation or replaced caller state")
	}
	if err := client.CallWithContext(t.Context(), "chat.delete", params, &result); err != nil || calls.Load() != 2 || result["marker"] != "confirmed" {
		t.Fatalf("explicit caller recovery failed: %v", err)
	}
}
