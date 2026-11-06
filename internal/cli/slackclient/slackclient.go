package slackclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/gechr/clog"
	"github.com/matcra587/slack-cli/api"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/config"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

// New builds a slack-go Client with the standard option set.
func New(_ context.Context, cmd *cobra.Command, runtime *cliruntime.RootRuntime, token string, opts ...slackgo.Option) *slackgo.Client {
	var noThrottle bool
	if cmd != nil {
		noThrottle, _ = cmd.Root().PersistentFlags().GetBool("no-throttle")
	}
	return api.NewSDKClient(token, api.SDKOptions{
		HTTPClient: runtime.HTTPClient,
		APIURL:     runtime.SlackBaseURL,
		Log:        runtime.Stderr,
		Debug:      clog.IsVerbose(),
		NoThrottle: noThrottle,
	}, opts...)
}

// NoThrottle returns a bare http.Client used by auth paths that do not need
// the proactive rate-limit transport.
func NoThrottle(runtime *cliruntime.RootRuntime) *http.Client {
	if runtime.HTTPClient != nil {
		return runtime.HTTPClient
	}
	return &http.Client{}
}

// Client resolves a token for the given profile and returns an authenticated
// slack-go client. NewRootCommand populates runtime.TokenResolver eagerly so
// callers can assume it is non-nil.
func Client(cmd *cobra.Command, profile config.WorkspaceProfile, runtime *cliruntime.RootRuntime) (*slackgo.Client, error) {
	token, err := runtime.TokenResolver.ResolveToken(cmd.Context(), profile)
	if err != nil {
		if errors.Is(err, config.ErrCredentialNotFound) {
			workspace := profile.Name
			if workspace == "" {
				workspace = "selected workspace"
			}
			return nil, fmt.Errorf("workspace %s is not authenticated; run slick auth login or switch to an authenticated workspace: %w", workspace, err)
		}
		return nil, err
	}
	return New(cmd.Context(), cmd, runtime, token), nil
}
