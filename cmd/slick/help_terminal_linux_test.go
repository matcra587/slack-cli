package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	cobracli "github.com/gechr/clib/cli/cobra"
	"golang.org/x/sys/unix"
)

func TestHelpPreservesRealTerminalFormatting(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("COLORTERM", "truecolor")
	for _, color := range []bool{false, true} {
		for _, width := range []uint16{42, 100} {
			for _, path := range [][]string{nil, {"history", "list"}} {
				t.Run(fmt.Sprintf("color=%t/width=%d/%s", color, width, strings.Join(path, "-")), func(t *testing.T) {
					if color {
						t.Setenv("CLICOLOR_FORCE", "1")
						t.Setenv("NO_COLOR", "")
					} else {
						t.Setenv("CLICOLOR_FORCE", "")
						t.Setenv("NO_COLOR", "1")
					}
					actual := captureHelpTerminal(t, path, width, true)
					expected := captureHelpTerminal(t, path, width, false)
					if len(actual) == 0 || !bytes.Equal(actual, expected) {
						i := 0
						for i < len(actual) && i < len(expected) && actual[i] == expected[i] {
							i++
						}
						aend, eend := min(i+180, len(actual)), min(i+180, len(expected))
						t.Fatalf("protected help changed at byte%d: got%q want%q", i, actual[i:aend], expected[i:eend])
					}
				})
			}
		}
	}
}

func captureHelpTerminal(t *testing.T, path []string, width uint16, protected bool) []byte {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = master.Close() }()
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = slave.Close() }()
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 24, Col: width}); err != nil {
		t.Fatal(err)
	}
	var output, diagnostic bytes.Buffer
	done := make(chan error, 1)
	go func() { _, err := io.Copy(&output, master); done <- err }()
	root, runtime := NewRootCommandWithRuntime(WithConfig(completionProfileConfig()), WithTTY(true), WithIO(strings.NewReader(""), slave, &diagnostic))
	if !protected {
		target, _, err := root.Find(path)
		if err != nil {
			t.Fatal(err)
		}
		target.SetHelpFunc(cobracli.HelpFunc(runtime.HelpRenderer, cobracli.Sections))
	}
	root.SetArgs(append(append([]string(nil), path...), "--help"))
	_, runErr := executeRoot(t.Context(), root, runtime)
	if err := slave.Close(); err != nil {
		t.Fatal(err)
	}
	readErr := <-done
	if runErr != nil || (readErr != nil && !errors.Is(readErr, unix.EIO)) || diagnostic.Len() != 0 {
		t.Fatalf("real terminal help did not execute: %v; read=%v", runErr, readErr)
	}
	return output.Bytes()
}
