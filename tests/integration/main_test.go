//go:build integration

package integration_test

import (
	"fmt"
	"os"
	"sync"
	"testing"
)

// The binary belongs to this test-process invocation, rather than the first
// caller's t.TempDir. Every new invocation builds fresh source, including mutants.
var sharedBinary struct {
	once      sync.Once
	directory string
	path      string
	err       error
}

func TestMain(m *testing.M) {
	code := m.Run()
	if sharedBinary.directory != "" {
		if err := os.RemoveAll(sharedBinary.directory); err != nil {
			fmt.Fprintln(os.Stderr, "remove integration binary:", err)
			code = 1
		}
	}
	os.Exit(code)
}
