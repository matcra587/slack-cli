package main

import (
	"errors"
	"net"
	"os"
	"strings"
)

// requiredField returns a huh validation function that rejects blank input.
// Also has a private copy in internal/cli/auth; collapse when config moves in Phase 09 Group D.
func requiredField(name string) func(string) error {
	return func(value string) error {
		if strings.TrimSpace(value) == "" {
			return errors.New(name + " is required")
		}
		return nil
	}
}

// usesTerminalFiles reports whether stdin and stderr are OS files.
// Also has a private copy in internal/cli/auth; collapse when config moves in Phase 09 Group D.
func usesTerminalFiles(runtime *RootRuntime) bool {
	_, stdinIsFile := runtime.Stdin.(*os.File)
	_, stderrIsFile := runtime.Stderr.(*os.File)
	return stdinIsFile && stderrIsFile
}

// defaultManifestOAuthRedirectURL returns the redirect URL for manifest generation.
// Also has a private copy in internal/cli/auth; collapse when manifest moves in Phase 09 Group D.
func defaultManifestOAuthRedirectURL() string {
	port := defaultOAuthCallbackPort()
	if port == osAssignedCallbackPort {
		if allocated, err := allocateLocalOAuthCallbackPort(); err == nil {
			port = allocated
		}
	}
	return oauthRedirectURLForPort(port)
}

// allocateLocalOAuthCallbackPort probes for a free local TCP port.
// Also has a private copy in internal/cli/auth; collapse when manifest moves in Phase 09 Group D.
func allocateLocalOAuthCallbackPort() (string, error) {
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

// oauthRedirectURLForPort assembles the loopback redirect URL for a given port.
// Also has a private copy in internal/cli/auth; collapse when manifest moves in Phase 09 Group D.
func oauthRedirectURLForPort(port string) string {
	port = strings.TrimSpace(port)
	if port == "" {
		port = defaultOAuthCallbackPort()
	}
	return "http://localhost:" + port + defaultOAuthCallbackPath
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func defaultOAuthCallbackPort() string {
	if port := strings.TrimSpace(os.Getenv("SLACK_CLI_CALLBACK_PORT")); port != "" {
		return port
	}
	return osAssignedCallbackPort
}

const (
	defaultOAuthCallbackPath = "/callback"
	osAssignedCallbackPort   = "0"
)
