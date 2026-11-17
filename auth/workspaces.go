package auth

import (
	"context"
	"errors"
	"os"
	"sort"

	"github.com/matcra587/slack-cli/internal/config"
)

// Workspace is non-secret metadata for one saved slick sign-in.
// Discovery does not resolve credentials or verify Slack authentication.
type Workspace struct {
	Name      string `json:"name"`
	TeamID    string `json:"team_id"`
	TeamName  string `json:"team_name"`
	TokenType string `json:"token_type"`
}

type WorkspaceList struct {
	DefaultWorkspace string      `json:"default_workspace"`
	Workspaces       []Workspace `json:"workspaces"`
}

// ListWorkspaces lists saved metadata without opening a keychain, resolving a
// token or secret-manager reference, refreshing OAuth, or contacting Slack.
func ListWorkspaces(ctx context.Context, opts Options) (WorkspaceList, error) {
	result := WorkspaceList{Workspaces: []Workspace{}}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	path, err := configPath(opts.ConfigPath)
	if err != nil {
		return result, unavailable(err)
	}
	cfg, err := config.LoadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, unavailable(err)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if name, err := cfg.ResolveWorkspaceName(""); err == nil {
		result.DefaultWorkspace = name
	}
	for name, profile := range cfg.Workspaces {
		result.Workspaces = append(result.Workspaces, Workspace{Name: name, TeamID: profile.TeamID, TeamName: profile.TeamName, TokenType: string(profile.TokenType)})
	}
	sort.Slice(result.Workspaces, func(i, j int) bool { return result.Workspaces[i].Name < result.Workspaces[j].Name })
	return result, nil
}
