// Package runtimetest provides shared test helpers for command packages.
//
// It consolidates the runtime + cobra root scaffolding that command-package
// tests use to exercise individual command trees against a fake Slack server
// or in pure-CLI mode. The behavior here mirrors what each per-package
// buildTestRoot/executeTestRoot used to do; only the boilerplate location
// has changed.
package runtimetest

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/gechr/clib/help"
	"github.com/gechr/clib/theme"
	"github.com/spf13/cobra"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/config"
)

// FixedNow is the deterministic timestamp used when Options.Now is nil.
var FixedNow = time.Date(2026, 5, 3, 13, 8, 0, 0, time.UTC)

// Options drives runtime + root construction.
type Options struct {
	// Config seeds runtime.Config; nil leaves it unset.
	Config *config.Config
	// SlackBaseURL seeds runtime.SlackBaseURL; empty leaves it unset.
	SlackBaseURL string
	// Stdin seeds runtime.Stdin; nil defaults to an empty reader.
	Stdin io.Reader
	// Token is returned by the default TokenResolver. Empty defaults to "xox-test".
	Token string
	// Now seeds runtime.Now; nil defaults to a closure returning FixedNow.
	Now func() time.Time
	// RequestID seeds runtime.RequestID; nil defaults to a closure returning "test-request".
	RequestID func() string
	// Theme, when set, seeds runtime.Theme and runtime.HelpRenderer. Tests for
	// commands that render help (agent guide, manifest schema, etc.) need this.
	Theme *theme.Theme
}

// NewRuntime builds a *RootRuntime with deterministic test defaults.
// Stdout and Stderr are returned as fresh bytes.Buffers so callers can wire
// them through NewRoot and assert on them after Run. The testing.TB argument
// is optional; pass nil from helpers that build a root outside of a test
// (e.g. cobra-only flag-introspection helpers).
func NewRuntime(tb testing.TB, opts Options) (runtime *cliruntime.RootRuntime, stdout, stderr *bytes.Buffer) {
	if tb != nil {
		tb.Helper()
	}

	stdout = &bytes.Buffer{}
	stderr = &bytes.Buffer{}

	stdin := opts.Stdin
	if stdin == nil {
		stdin = strings.NewReader("")
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return FixedNow }
	}
	requestID := opts.RequestID
	if requestID == nil {
		requestID = func() string { return "test-request" }
	}
	token := opts.Token
	if token == "" {
		token = "xox-test"
	}

	runtime = &cliruntime.RootRuntime{
		Stdin:     stdin,
		Stdout:    stdout,
		Stderr:    stderr,
		IsTTY:     false,
		Now:       now,
		RequestID: requestID,
	}
	if opts.Config != nil {
		runtime.Config = opts.Config
	}
	if opts.SlackBaseURL != "" {
		runtime.SlackBaseURL = opts.SlackBaseURL
	}
	if opts.Theme != nil {
		runtime.Theme = opts.Theme
		runtime.HelpRenderer = help.NewRenderer(opts.Theme)
	}
	runtime.TokenResolver = cliruntime.TokenResolverFunc(func(_ context.Context, _ config.WorkspaceProfile) (string, error) {
		return token, nil
	})

	return runtime, stdout, stderr
}

// NewRoot builds a *cobra.Command with the standard slick persistent flags
// registered and IO wired to the runtime. Caller adds command trees via
// root.AddCommand. The persistent flag set matches the production root
// (cmd/slick.NewRootCommand) for all flags consulted by command handlers
// via cliruntime.CommandContext.
func NewRoot(runtime *cliruntime.RootRuntime, stdout, stderr *bytes.Buffer) *cobra.Command {
	root := &cobra.Command{
		Use:           "slick",
		Short:         "Slack command line interface",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetIn(runtime.Stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)

	flags := root.PersistentFlags()
	flags.StringP("workspace", "w", "", "Workspace profile")
	flags.StringP("output", "o", clioutput.OutputAuto, "Output format: auto, human, json, compact")
	flags.BoolP("dry-run", "n", false, "Preview mutating commands without changing Slack")
	flags.BoolP("no-throttle", "Q", false, "Disable proactive Slack API throttling")
	flags.BoolP("debug", "D", false, "Enable debug-level output")

	return root
}

// Run executes the command tree and returns captured stdout/stderr. The
// caller is expected to have wired stdin via NewRuntime's Options.Stdin so
// that both runtime.Stdin and root.In point at the same source. Run does not
// reset stdin or buffers; callers that want a clean slate should construct a
// fresh runtime + root per invocation, mirroring the original
// buildTestRoot/executeTestRoot semantics.
func Run(tb testing.TB, root *cobra.Command, args []string, stdout, stderr *bytes.Buffer) (stdoutStr, stderrStr string, err error) {
	if tb != nil {
		tb.Helper()
	}
	root.SetArgs(args)
	err = root.Execute()
	return stdout.String(), stderr.String(), err
}
