package main

import (
	"encoding/json"
	"testing"

	clichannel "github.com/matcra587/slack-cli/internal/cli/channel"
	clihistory "github.com/matcra587/slack-cli/internal/cli/history"
	climessage "github.com/matcra587/slack-cli/internal/cli/message"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	clisearch "github.com/matcra587/slack-cli/internal/cli/search"
	cliuser "github.com/matcra587/slack-cli/internal/cli/user"
)

func TestBareRootHonorsMachineOutputWithoutWorkspace(t *testing.T) {
	for _, mode := range []string{"json", "compact"} {
		t.Run(mode, func(t *testing.T) {
			stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"--output=" + mode})
			if err != nil || stderr != "" {
				t.Fatalf("error=%v stderr=%q", err, stderr)
			}
			var result map[string]json.RawMessage
			if err := json.Unmarshal([]byte(stdout), &result); err != nil {
				t.Fatalf("root output is not JSON: %v: %s", err, stdout)
			}
			if mode == "json" {
				if string(result["ok"]) != "true" {
					t.Fatalf("root did not report success: %s", stdout)
				}
				if err := json.Unmarshal(result["data"], &result); err != nil {
					t.Fatal(err)
				}
			}
			if len(result["commands"]) == 0 {
				t.Fatalf("root discovery omitted commands: %s", stdout)
			}
		})
	}
}

func TestPaginatedOperationsRetainCursorInBothMachineModes(t *testing.T) {
	for _, tc := range []struct {
		command string
		data    any
	}{
		{"history.list", clihistory.Data{}},
		{"lookup.channel", clichannel.ListData{}},
		{"lookup.user", cliuser.ListData{}},
		{"search.messages", clisearch.Data{}},
		{"message.scheduled.list", climessage.ScheduledListData{}},
	} {
		for _, mode := range []RenderMode{RenderModeEnvelope, RenderModeCompact} {
			name := tc.command + map[RenderMode]string{RenderModeEnvelope: "/json", RenderModeCompact: "/compact"}[mode]
			t.Run(name, func(t *testing.T) {
				ctx, stdout, stderr := newOutputTestContext(mode)
				next := "next-page"
				pagination := &clioutput.Pagination{NextCursor: &next, HasMore: true}
				if err := ctx.WriteResultWithPagination(tc.command, tc.data, pagination); err != nil {
					t.Fatal(err)
				}
				var value struct {
					Data json.RawMessage `json:"data"`
					Meta struct {
						Pagination clioutput.Pagination `json:"pagination"`
					} `json:"meta"`
					Pagination clioutput.Pagination `json:"pagination"`
				}
				if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
					t.Fatal(err)
				}
				if mode == RenderModeEnvelope {
					if value.Meta.Pagination.NextCursor == nil || *value.Meta.Pagination.NextCursor != next {
						t.Fatal("legacy meta.pagination cursor was lost")
					}
					if err := json.Unmarshal(value.Data, &value); err != nil {
						t.Fatal(err)
					}
				}
				if value.Pagination.NextCursor == nil || *value.Pagination.NextCursor != next || !value.Pagination.HasMore || stderr.Len() != 0 {
					t.Fatalf("paging facts missing or unexpected stderr: stdout=%s stderr=%s", stdout, stderr)
				}
			})
		}
	}
}

func TestFailureOutputSelectionRespectsValuesAndTerminator(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"after unknown command", []string{"missing", "--output=human"}, "human"},
		{"short attached", []string{"-ojson", "missing"}, "json"},
		{"separate value", []string{"missing", "-o", "compact"}, "compact"},
		{"literal message value", []string{"message", "send", "--message", "--output=human"}, "auto"},
		{"literal terminator", []string{"--", "--output=human"}, "auto"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := failureOutputMode(NewRootCommand(), tc.args); got != tc.want {
				t.Fatalf("mode=%q, want %q", got, tc.want)
			}
		})
	}
}
