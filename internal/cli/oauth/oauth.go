// Package oauth provides shared OAuth callback URL helpers and form helpers
// used by the auth, config, and manifest CLI command packages.
package oauth

import (
	"errors"
	"net"
	"os"
	"strings"

	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
)

const (
	// DefaultCallbackPath is the loopback callback path appended to the redirect URL.
	DefaultCallbackPath = "/callback"
	// OSAssignedCallbackPort is the placeholder used to ask the OS to allocate a free port.
	OSAssignedCallbackPort = "0"
)

// RequiredField returns a huh validation function that rejects blank input.
func RequiredField(name string) func(string) error {
	return func(value string) error {
		if strings.TrimSpace(value) == "" {
			return errors.New(name + " is required")
		}
		return nil
	}
}

// UsesTerminalFiles reports whether stdin and stderr are OS files (i.e. a real
// terminal is attached). Used to decide whether interactive forms run in
// accessible mode.
func UsesTerminalFiles(runtime *cliruntime.RootRuntime) bool {
	_, stdinIsFile := runtime.Stdin.(*os.File)
	_, stderrIsFile := runtime.Stderr.(*os.File)
	return stdinIsFile && stderrIsFile
}

// DefaultManifestRedirectURL returns the redirect URL for manifest generation,
// allocating a concrete loopback port if none has been configured.
func DefaultManifestRedirectURL() string {
	port := DefaultCallbackPort()
	if port == OSAssignedCallbackPort {
		if allocated, err := AllocateLocalCallbackPort(); err == nil {
			port = allocated
		}
	}
	return RedirectURLForPort(port)
}

// AllocateLocalCallbackPort probes for a free local TCP port.
func AllocateLocalCallbackPort() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	_, port, splitErr := net.SplitHostPort(listener.Addr().String())
	closeErr := listener.Close()
	if splitErr != nil {
		return "", splitErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return port, nil
}

// RedirectURLForPort assembles the loopback redirect URL for a given port.
func RedirectURLForPort(port string) string {
	port = strings.TrimSpace(port)
	if port == "" {
		port = DefaultCallbackPort()
	}
	return "http://localhost:" + port + DefaultCallbackPath
}

// DefaultCallbackPort returns the configured callback port, or OSAssignedCallbackPort
// when none is configured.
func DefaultCallbackPort() string {
	if port := strings.TrimSpace(os.Getenv("SLACK_CLI_CALLBACK_PORT")); port != "" {
		return port
	}
	return OSAssignedCallbackPort
}
