package testutil

import (
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// NewTerminal opens a real terminal writer and captures its output. Read closes
// the writer and drains the capture; cleanup also joins the reader on failures.
func NewTerminal(t testing.TB) (writer *os.File, read func() string) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = master.Close() })
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	writer, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	var data []byte
	var readErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		data, readErr = io.ReadAll(master)
	}()
	t.Cleanup(func() {
		_ = writer.Close()
		_ = master.Close()
		<-done
	})
	return writer, func() string {
		t.Helper()
		_ = writer.Close()
		<-done
		// Linux returns EIO after the final slave closes; preceding bytes are valid.
		if readErr != nil && !errors.Is(readErr, unix.EIO) {
			t.Fatal(readErr)
		}
		return string(data)
	}
}
