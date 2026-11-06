//go:build integration && (linux || darwin)

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBinaryStatusClockUsesProcessTimezoneWithoutChangingMetadataUTC(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, zone := range []string{"America/Toronto", "Pacific/Auckland"} {
		t.Run(zone, func(t *testing.T) {
			location, err := time.LoadLocation(zone)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "config.toml")
			prior := []byte("schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\n")
			if err := os.WriteFile(path, prior, 0o600); err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "status", "set", "--text=Review", "--until=22:00", "--dry-run", "--output=json", "--color=never")
			cmd.Env = append(isolatedRuntimeEnv(t), "SLICK_CONFIG="+path, "SLACK_CLI_BASE_URL="+server.URL, "TZ="+zone)
			var out, diagnostic bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &diagnostic
			before := time.Now().In(location)
			if err := cmd.Run(); err != nil || diagnostic.Len() != 0 {
				t.Fatalf("local-clock preview failed: %v", err)
			}
			after := time.Now().In(location)
			var frame struct {
				OK   bool
				Meta struct{ Command, Timestamp string }
				Data struct {
					Text       string
					Expiration int64
				}
			}
			if json.Unmarshal(out.Bytes(), &frame) != nil || !frame.OK || frame.Meta.Command != "status.set" || frame.Data.Text != "Review" {
				t.Fatal("status preview lost its structured receipt")
			}
			expiration := time.Unix(frame.Data.Expiration, 0).In(location)
			if expiration.Hour() != 22 || expiration.Minute() != 0 || expiration.Second() != 0 || expiration.Before(before) || expiration.After(after.AddDate(0, 0, 1)) {
				t.Fatalf("clock expiration did not select the upcoming local 22:00 in %s: %s", zone, expiration.Format(time.RFC3339))
			}
			if !strings.HasSuffix(frame.Meta.Timestamp, "Z") {
				t.Fatal("local status input changed the default UTC envelope timestamp")
			}
			preserved, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(preserved, prior) || requests.Load() != 0 {
				t.Fatal("status preview changed config or requested Slack")
			}
		})
	}
}
