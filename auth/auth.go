// Package auth resolves the credentials saved by slick auth login.
//
// Resolving credentials is explicit: importing this package does not inspect
// config files, unlock a keychain, invoke a secret manager, or contact Slack.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	xfilepath "github.com/gechr/x/filepath"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/credentials"
)

// ErrNotAuthenticated identifies a failure to resolve usable slick credentials.
// Use errors.Is to distinguish it from a canceled context.
var ErrNotAuthenticated = errors.New("slick credentials unavailable; run slick auth login")

// CredentialStore provides the keychain operations used by slick. A supplied
// store overrides the OS keychain, including when refreshing OAuth credentials.
type CredentialStore interface {
	Set(service, user, secret string) error
	Get(service, user string) (string, error)
	Delete(service, user string) error
}

// SecretReader reads an explicitly configured secret-manager reference.
// The default reader runs op read only for a selected op:// reference.
type SecretReader interface {
	Read(context.Context, string) (string, error)
}

// Options selects a saved workspace and the services used to resolve its token.
// Empty ConfigPath follows SLICK_CONFIG, SLACK_CLI_CONFIG, then XDG config home.
// Workspace overrides SLACK_CLI_DEFAULT_WORKSPACE. When both are empty,
// resolution selects the config's saved default workspace.
type Options struct {
	Workspace       string
	ConfigPath      string
	HTTPClient      *http.Client
	CredentialStore CredentialStore
	SecretReader    SecretReader
	Now             func() time.Time
	// SlackBaseURL overrides Slack's API base URL for OAuth refresh. Leave it
	// empty in production; fixtures can supply a local server explicitly.
	SlackBaseURL string
}

// Session contains the selected workspace's identity and resolved access token.
// Its JSON and formatted representations contain metadata only. Call Token to
// pass the credential to a Slack client; never log or persist that return value.
type Session struct {
	Workspace string `json:"workspace"`
	TeamID    string `json:"team_id,omitempty"`
	TeamName  string `json:"team_name,omitempty"`
	TokenType string `json:"token_type,omitempty"`
	token     string
}

// Token returns the access token for use in an authenticated client.
func (s Session) Token() string { return s.token }

// String returns safe workspace metadata without any credential values.
func (s Session) String() string {
	raw, _ := s.MarshalJSON()
	return string(raw)
}

// GoString keeps %#v formatting from inspecting private credential fields.
func (s Session) GoString() string { return s.String() }

// MarshalJSON includes only reviewed identity fields, never token material.
func (s Session) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Workspace string `json:"workspace"`
		TeamID    string `json:"team_id,omitempty"`
		TeamName  string `json:"team_name,omitempty"`
		TokenType string `json:"token_type,omitempty"`
	}{s.Workspace, s.TeamID, s.TeamName, s.TokenType})
}

// Resolve resolves the selected saved credential without changing workspace
// preferences. Expiring OAuth credentials are refreshed through the same path
// used by slick; a failed refresh does not replace the stored credential.
func Resolve(ctx context.Context, opts Options) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	path, err := configPath(opts.ConfigPath)
	if err != nil {
		return Session{}, unavailable(err)
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		return Session{}, unavailable(err)
	}
	workspace, err := cfg.ResolveWorkspace(strings.TrimSpace(opts.Workspace))
	if err != nil {
		return Session{}, unavailable(err)
	}
	store := opts.CredentialStore
	if store == nil {
		store = config.NewKeyringCredentialStore()
	}
	resolver := credentials.CredentialTokenResolver{
		Store: store, HTTPClient: opts.HTTPClient, SlackBaseURL: opts.SlackBaseURL,
		SecretReader: opts.SecretReader, Now: opts.Now,
	}
	value, err := resolver.ResolveToken(ctx, workspace)
	if err != nil {
		if ctx.Err() != nil {
			return Session{}, ctx.Err()
		}
		return Session{}, unavailable(err)
	}
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	if strings.TrimSpace(value) == "" {
		return Session{}, unavailable(config.ErrCredentialNotFound)
	}
	return Session{
		Workspace: workspace.Name, TeamID: workspace.TeamID,
		TeamName: workspace.TeamName, TokenType: string(workspace.TokenType), token: value,
	}, nil
}

func configPath(explicit string) (string, error) {
	if explicit != "" {
		return xfilepath.Expand(explicit), nil
	}
	for _, name := range []string{"SLICK_CONFIG", "SLACK_CLI_CONFIG"} {
		if path := os.Getenv(name); path != "" {
			return xfilepath.Expand(path), nil
		}
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "slick", "config.toml"), nil
}

// resolutionError keeps provider diagnostics out of normal logs while allowing
// callers to inspect the underlying error with errors.Is or errors.As.
type resolutionError struct{ cause error }

func (e resolutionError) Error() string    { return ErrNotAuthenticated.Error() }
func (e resolutionError) String() string   { return e.Error() }
func (e resolutionError) GoString() string { return fmt.Sprintf("auth.Error(%q)", e.Error()) }
func (e resolutionError) Unwrap() []error  { return []error{ErrNotAuthenticated, e.cause} }

func unavailable(cause error) error { return resolutionError{cause: cause} }
