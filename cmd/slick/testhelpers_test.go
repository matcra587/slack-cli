package main

import (
	"io"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func executeAuthRoot(t *testing.T, cfg *config.Config, configPath string, store config.CredentialStore, baseURL string, args []string) (string, string, error) {
	t.Helper()
	return executeAuthRootWithInput(t, cfg, configPath, store, baseURL, strings.NewReader(""), false, args)
}

func executeAuthRootWithInput(t *testing.T, cfg *config.Config, configPath string, store config.CredentialStore, baseURL string, stdin io.Reader, isTTY bool, args []string) (string, string, error) {
	t.Helper()
	return executeAuthRootWithOptions(t, cfg, configPath, store, baseURL, stdin, isTTY, args)
}

func executeAuthRootWithOptions(t *testing.T, cfg *config.Config, configPath string, store config.CredentialStore, baseURL string, stdin io.Reader, isTTY bool, args []string, options ...RootOption) (string, string, error) {
	t.Helper()
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	rootOptions := []RootOption{
		WithConfig(cfg),
		WithConfigPath(configPath),
		WithCredentialStore(store),
		WithSlackBaseURL(baseURL),
		WithIO(stdin, stdout, stderr),
		WithTTY(isTTY),
	}
	rootOptions = append(rootOptions, options...)
	cmd := NewRootCommand(rootOptions...)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

type lineByLineReader struct {
	lines []string
}

func lineReader(value string) *lineByLineReader {
	return &lineByLineReader{lines: strings.SplitAfter(value, "\n")}
}

func (r *lineByLineReader) Read(p []byte) (int, error) {
	if len(r.lines) == 0 {
		return 0, io.EOF
	}
	line := r.lines[0]
	r.lines = r.lines[1:]
	return copy(p, line), nil
}

func authTestConfig() *config.Config {
	return &config.Config{
		SchemaVersion:    config.SchemaVersion,
		DefaultWorkspace: "default",
		Workspaces: map[string]config.WorkspaceProfile{
			"default": {Name: "default", TeamID: "T123", TokenType: config.TokenTypeBot, TokenRef: "keychain:slack-cli/default"},
			"other":   {Name: "other", TeamID: "T999", TokenType: config.TokenTypeUser, TokenRef: "keychain:slack-cli/other"},
		},
	}
}
