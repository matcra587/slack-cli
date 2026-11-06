package output_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gechr/clog"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
)

func TestHumanPaginationFooterWriteFailuresReturnIOAndKeepPriorRows(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		render func(*clioutput.CommandContext, *clioutput.Pagination) error
	}{
		{"messages", func(c *clioutput.CommandContext, p *clioutput.Pagination) error {
			return c.WriteMessages("history.list", []clioutput.Message{{TS: "1746284582.123456", Text: new("Review complete")}}, p)
		}},
		{"search", func(c *clioutput.CommandContext, p *clioutput.Pagination) error {
			return c.WriteSearch("search.messages", []clioutput.SearchMessage{{TS: "1746284582.123456", Text: "Review complete"}}, false, p)
		}},
		{"channels", func(c *clioutput.CommandContext, p *clioutput.Pagination) error {
			return c.WriteChannels("lookup.channel", []clioutput.Channel{{ID: "C1", Name: "Review complete"}}, p)
		}},
		{"users", func(c *clioutput.CommandContext, p *clioutput.Pagination) error {
			return c.WriteUsers("lookup.user", []clioutput.User{{ID: "U1", Name: "Review complete"}}, p)
		}},
		{"scheduled", func(c *clioutput.CommandContext, p *clioutput.Pagination) error {
			return c.WriteScheduledMessages("message.scheduled.list", []clioutput.ScheduledMessage{{ID: "Q1", TextPreview: "Review complete"}}, p)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, failTable := range []bool{false, true} {
				var diagnostic bytes.Buffer
				destination := &paginationFailWriter{failTable: failTable}
				outlog, errlog := clioutput.BuildBaseLoggers(destination, &diagnostic, clog.ColorNever)
				c := &clioutput.CommandContext{Mode: clioutput.RenderModePlain, Stdout: destination, Stderr: &diagnostic, StdoutLog: outlog, StderrLog: errlog, ColorMode: clog.ColorNever}
				err := tc.render(c, &clioutput.Pagination{NextCursor: new("page-next"), HasMore: true, ItemsReturned: new(1)})
				commandErr, ok := errors.AsType[clioutput.CommandError](err)
				if !ok || commandErr.CLIError.ExitCode != 8 {
					t.Fatalf("lost output-write failure: %v", err)
				}
				if failTable {
					if destination.footerAttempts != 0 {
						t.Fatal("footer written after table failure")
					}
				} else if destination.footerAttempts != 1 || !strings.Contains(destination.accepted.String(), "Review complete") {
					t.Fatal("footer write was replayed or prior rows changed")
				}
			}
		})
	}
}

type paginationFailWriter struct {
	accepted       bytes.Buffer
	failTable      bool
	footerAttempts int
}

func (w *paginationFailWriter) Write(p []byte) (int, error) {
	footer := bytes.Contains(p, []byte("has_more="))
	if footer {
		w.footerAttempts++
	}
	if footer || w.failTable {
		return 0, io.ErrClosedPipe
	}
	return w.accepted.Write(p)
}

func TestHumanPaginationKeepsFalseAndZeroInPlainAndColoredOutput(t *testing.T) {
	t.Parallel()
	for _, mode := range []clog.ColorMode{clog.ColorNever, clog.ColorAlways} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			t.Parallel()
			var out, diagnostic bytes.Buffer
			outlog, errlog := clioutput.BuildBaseLoggers(&out, &diagnostic, mode)
			c := &clioutput.CommandContext{Stdout: &out, StdoutLog: outlog, StderrLog: errlog, ColorMode: mode, IsTTY: true}
			if err := c.WriteMessages("history.list", nil, &clioutput.Pagination{HasMore: false, ItemsReturned: new(0)}); err != nil {
				t.Fatal(err)
			}
			visible := ansi.Strip(out.String())
			if !strings.Contains(visible, "has_more=false") || !strings.Contains(visible, "items_returned=0") {
				t.Fatalf("pagination fields disappeared in %v: %q", mode, visible)
			}
			if mode == clog.ColorAlways && visible == out.String() {
				t.Fatal("colored field rendering was not exercised")
			}
		})
	}
}
