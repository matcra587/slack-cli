package output_test

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gechr/clog"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"golang.org/x/sys/unix"
)

func TestHumanResultPreservesRealTerminalDetectionAndWidth(t *testing.T) {
	t.Parallel()
	for _, mode := range []clog.ColorMode{clog.ColorNever, clog.ColorAlways, clog.ColorAuto} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			t.Parallel()
			testHumanTerminal(t, mode)
		})
	}
}

func testHumanTerminal(t *testing.T, mode clog.ColorMode) {
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
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 24, Col: 52}); err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	outlog, errlog := clioutput.BuildBaseLoggers(slave, &diagnostic, mode)
	// If the wrapper loses terminal detection, this policy wrongly suppresses
	// Info result events. This is a real terminal even with color disabled.
	outlog.SetNonTTYLevel(clog.LevelWarn)
	c := &clioutput.CommandContext{Mode: clioutput.RenderModePlain, Stdout: slave, Stderr: &diagnostic, StdoutLog: outlog, StderrLog: errlog, ColorMode: mode, IsTTY: true}
	result := &terminalHumanResult{}
	if err := c.WriteResult("synthetic", result); err != nil {
		t.Fatal(err)
	}
	if !result.terminal || result.width != 52 || result.colorsDisabled != outlog.Output().ColorsDisabled() {
		t.Fatal("result lost real terminal detection or configured width")
	}
	if err := slave.Close(); err != nil {
		t.Fatal(err)
	}
	// Linux returns EIO after the slave closes; retain bytes read before it.
	var captured bytes.Buffer
	buf := make([]byte, 4096)
	for {
		n, readErr := master.Read(buf)
		captured.Write(buf[:n])
		if readErr != nil {
			break
		}
	}
	visible := ansi.Strip(captured.String())
	for line := range strings.SplitSeq(visible, "\n") {
		if ansi.StringWidth(strings.TrimSuffix(line, "\r")) > 52 {
			t.Fatal("terminal table exceeded the actual terminal width")
		}
	}
	if !strings.Contains(visible, "Terminal result") || !strings.Contains(visible, "Review") || diagnostic.Len() != 0 {
		t.Fatal("terminal output was suppressed or lost its table")
	}
}

type terminalHumanResult struct {
	terminal       bool
	width          int
	colorsDisabled bool
}

func (d *terminalHumanResult) WritePlain(c *clioutput.CommandContext, _ string, _ *clioutput.Pagination) error {
	output := c.StdoutLogger().Output()
	d.terminal, d.width = output.IsTTY(), output.Width()
	d.colorsDisabled = output.ColorsDisabled()
	c.StdoutLogger().Info().Msg("Terminal result")
	return c.WriteMessageTable([]clioutput.Message{{TS: "1746284582.123456", Text: new(strings.Repeat("Review ", 30))}})
}
