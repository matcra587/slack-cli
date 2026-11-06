package slackclient

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gechr/clog"
	"github.com/matcra587/slack-cli/api"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
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

// Client resolves a token for the given profile and returns an authenticated
// slack-go client. NewRootCommand populates runtime.TokenResolver eagerly so
// callers can assume it is non-nil.
func Client(cmd *cobra.Command, profile config.WorkspaceProfile, runtime *cliruntime.RootRuntime) (*slackgo.Client, error) {
	if err := cmd.Context().Err(); err != nil {
		return nil, credentialError{cause: err, workspace: profile.Name}
	}
	token, err := runtime.TokenResolver.ResolveToken(cmd.Context(), profile)
	if err != nil {
		return nil, credentialError{cause: err, workspace: profile.Name}
	}
	if err := cmd.Context().Err(); err != nil {
		return nil, credentialError{cause: err, workspace: profile.Name}
	}
	if strings.TrimSpace(token) == "" {
		return nil, credentialError{cause: config.ErrCredentialNotFound, workspace: profile.Name}
	}
	return New(cmd.Context(), cmd, runtime, token), nil
}

// CLIError classifies credential-resolution failures without exposing provider
// diagnostics, which may contain credentials or signed request URLs.
func CLIError(ctx context.Context, err error) clioutput.CLIError {
	if errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded {
		return clioutput.CLIError{Type: clioutput.ErrorTypeTimeout, Message: "timeout", ExitCode: clioutput.ExitCodeTimeout}
	}
	if errors.Is(err, context.Canceled) || ctx.Err() == context.Canceled {
		return clioutput.CLIError{Type: clioutput.ErrorTypeCanceled, Message: "canceled", ExitCode: clioutput.ExitCodeCanceled}
	}
	if credential, ok := errors.AsType[credentialError](err); ok {
		return clioutput.AuthCLIError(credential.Error())
	}
	return clioutput.AuthCLIError("could not resolve credentials; check the selected workspace's credential provider")
}

type credentialError struct {
	cause     error
	workspace string
}

func (e credentialError) Error() string {
	if errors.Is(e.cause, config.ErrLegacyCredential) {
		return config.ErrLegacyCredential.Error()
	}
	if errors.Is(e.cause, config.ErrCredentialNotFound) {
		workspace := e.workspace
		if workspace == "" {
			workspace = "selected workspace"
		}
		return fmt.Sprintf("workspace %s is not authenticated; run slick auth login or switch to an authenticated workspace", workspace)
	}
	return "could not resolve credentials; check the selected workspace's credential provider"
}

func (e credentialError) String() string { return e.Error() }
func (e credentialError) GoString() string {
	return fmt.Sprintf("slackclient.CredentialError(%q)", e.Error())
}
func (e credentialError) Unwrap() error { return e.cause }
