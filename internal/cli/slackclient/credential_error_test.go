package slackclient_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/cli/runtime/runtimetest"
	"github.com/matcra587/slack-cli/internal/cli/slackclient"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/spf13/cobra"
)

func TestCredentialErrorsPreserveCausesWithoutFormattingProviderSecrets(t *testing.T) {
	const secret = "synthetic-private-provider-data"
	for _, tc := range []struct {
		name  string
		cause error
	}{
		{"provider", errors.New(secret)},
		{"value provider", credentialProviderFailure(secret)},
		{"legacy", fmt.Errorf("%s: %w", secret, config.ErrLegacyCredential)},
		{"missing", fmt.Errorf("%s: %w", secret, config.ErrCredentialNotFound)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cause := tc.cause
			runtime, _, _ := runtimetest.NewRuntime(t, runtimetest.Options{})
			runtime.TokenResolver = cliruntime.TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) { return "", cause })
			cmd := &cobra.Command{}
			cmd.SetContext(context.Background())
			_, err := slackclient.Client(cmd, config.WorkspaceProfile{Name: "fixture"}, runtime)
			if err == nil || !errors.Is(err, cause) {
				t.Fatal("credential error lost its cause")
			}
			if strings.Contains(fmt.Sprintf("%s %v %+v %#v", err, err, err, err), secret) {
				t.Fatal("credential error formatting exposed provider data")
			}
		})
	}
}

type credentialProviderFailure string

func (failure credentialProviderFailure) Error() string { return string(failure) }
