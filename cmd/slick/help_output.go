package main

import (
	"context"
	"io"
	"os"

	"github.com/charmbracelet/colorprofile"
	"github.com/gechr/clib/help"
	"github.com/gechr/x/terminal"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/spf13/cobra"
)

// Cobra's help callback cannot return an error, including on the --help path
// that bypasses RunE. The application boundary checks its captured result.
func executeRoot(ctx context.Context, root *cobra.Command, runtime *RootRuntime) (*cobra.Command, error) {
	runtime.HelpOutputError = nil
	command, err := root.ExecuteContextC(ctx)
	if runtime.HelpOutputError != nil {
		return command, runtime.HelpOutputError
	}
	return command, err
}

func protectHelpOutput(cmd *cobra.Command, runtime *RootRuntime) {
	// Capture inherited callbacks before replacing the parent's callback; this
	// retains custom child sections without wrapping any callback twice.
	for _, child := range cmd.Commands() {
		protectHelpOutput(child, runtime)
	}
	render := cmd.HelpFunc()
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		runtime.HelpOutputError = nil
		dst := cmd.OutOrStdout()
		_, diagnostic := clioutput.BuildBaseLoggers(dst, runtime.Stderr, runtime.ColorMode)
		output := &CommandContext{Stdout: dst, Stderr: runtime.Stderr, StderrLog: diagnostic}
		// clib detects width through concrete file/colorprofile writers. Capture
		// that width before adding the failure observer and restore renderer state.
		renderer := *runtime.HelpRenderer
		help.WithMaxWidth(helpOutputWidth(dst))(runtime.HelpRenderer)
		defer func() { *runtime.HelpRenderer = renderer }()
		runtime.HelpOutputError = output.WriteToStdout(func(w io.Writer) {
			if file, ok := dst.(interface {
				io.ReadWriteCloser
				Fd() uintptr
			}); ok {
				w = &helpTerminalWriter{Writer: w, ReadCloser: file, descriptor: file.Fd()}
			}
			// Render a view so inherited writer settings on cmd stay intact.
			view := *cmd
			view.SetOut(w)
			render(&view, args)
		})
	})
}

func helpOutputWidth(w io.Writer) int {
	switch dst := w.(type) {
	case *os.File:
		return terminal.Width(dst)
	case *colorprofile.Writer:
		return helpOutputWidth(dst.Forward)
	default:
		return 0
	}
}

// colorprofile tests the full terminal file interface, not just Fd.
type helpTerminalWriter struct {
	io.Writer
	io.ReadCloser
	descriptor uintptr
}

func (w *helpTerminalWriter) Fd() uintptr { return w.descriptor }
