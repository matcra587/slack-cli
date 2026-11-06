package file_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	clifile "github.com/matcra587/slack-cli/internal/cli/file"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/cli/runtime/runtimetest"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestUploadMissingStdinReportsValidationBeforeCredentials(t *testing.T) {
	runtime, out, diagnostic := runtimetest.NewRuntime(t, runtimetest.Options{Config: workspaceConfig(config.TokenTypeUser), SlackBaseURL: "http://127.0.0.1:1"})
	runtime.Stdin = nil
	resolutions := 0
	runtime.TokenResolver = cliruntime.TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
		resolutions++
		return "", errors.New("unexpected credentials")
	})
	root := runtimetest.NewRoot(runtime, out, diagnostic)
	root.AddCommand(clifile.NewCommand(runtime))
	_, _, err := runtimetest.Run(t, root, []string{"file", "upload", "--channel=C123", "--file=-", "--filename=artifact.dat"}, out, diagnostic)
	if err == nil || diagnostic.Len() != 0 || resolutions != 0 || !strings.Contains(out.String(), `"type":"validation_error"`) || !strings.Contains(out.String(), "stdin is unavailable") {
		t.Fatalf("missing stdin changed validation contract: err=%v stdout=%s stderr=%s", err, out, diagnostic)
	}
}
