package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/gechr/clib/help"
	"github.com/gechr/clib/theme"
	"github.com/gechr/clog"
	"github.com/gechr/x/human"
	"github.com/matcra587/slack-cli/internal/config"
)

type RootRuntime struct {
	Config          *config.Config
	ConfigLoadError error
	ConfigExplicit  bool
	ConfigPath      string
	CredentialStore config.CredentialStore
	TokenResolver   TokenResolver
	SlackBaseURL    string
	HTTPClient      *http.Client
	OpenURL         func(string) error
	OAuthTimeout    time.Duration
	Timeout         time.Duration
	CancelTimeout   context.CancelFunc
	Stdin           io.Reader
	Stdout          io.Writer
	Stderr          io.Writer
	IsTTY           bool
	ColorMode       clog.ColorMode
	Now             func() time.Time
	RequestID       func() string
	Theme           *theme.Theme
	HelpRenderer    *help.Renderer
}

type RootOption func(*RootRuntime)

func WithConfig(cfg *config.Config) RootOption {
	return func(runtime *RootRuntime) {
		runtime.Config = cfg
		runtime.ConfigLoadError = nil
		runtime.ConfigExplicit = true
	}
}

// WithConfigPath stores the path and, when ConfigExplicit is unset, eagerly
// loads the config file. The load error is captured into ConfigLoadError so
// callers can defer surfacing it; the option itself never returns an error.
func WithConfigPath(path string) RootOption {
	return func(runtime *RootRuntime) {
		runtime.ConfigPath = path
		if !runtime.ConfigExplicit {
			runtime.Config, runtime.ConfigLoadError = LoadDefaultConfig(path)
		}
	}
}

func WithCredentialStore(store config.CredentialStore) RootOption {
	return func(runtime *RootRuntime) {
		runtime.CredentialStore = store
	}
}

func WithTokenResolver(resolver TokenResolver) RootOption {
	return func(runtime *RootRuntime) {
		runtime.TokenResolver = resolver
	}
}

func WithSlackBaseURL(baseURL string) RootOption {
	return func(runtime *RootRuntime) {
		runtime.SlackBaseURL = baseURL
	}
}

func WithIO(stdin io.Reader, stdout, stderr io.Writer) RootOption {
	return func(runtime *RootRuntime) {
		runtime.Stdin = stdin
		runtime.Stdout = stdout
		runtime.Stderr = stderr
	}
}

func WithTTY(isTTY bool) RootOption {
	return func(runtime *RootRuntime) {
		runtime.IsTTY = isTTY
	}
}

func WithNow(now func() time.Time) RootOption {
	return func(runtime *RootRuntime) {
		runtime.Now = now
	}
}

func WithRequestID(requestID func() string) RootOption {
	return func(runtime *RootRuntime) {
		runtime.RequestID = requestID
	}
}

func WithURLOpener(openURL func(string) error) RootOption {
	return func(runtime *RootRuntime) {
		runtime.OpenURL = openURL
	}
}

func WithOAuthTimeout(timeout time.Duration) RootOption {
	return func(runtime *RootRuntime) {
		runtime.OAuthTimeout = timeout
	}
}

type TokenResolver interface {
	ResolveToken(ctx context.Context, profile config.WorkspaceProfile) (string, error)
}

type TokenResolverFunc func(ctx context.Context, profile config.WorkspaceProfile) (string, error)

func (f TokenResolverFunc) ResolveToken(ctx context.Context, profile config.WorkspaceProfile) (string, error) {
	return f(ctx, profile)
}

func LoadDefaultConfig(path string) (*config.Config, error) {
	if path == "" {
		return nil, nil
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, missingConfigError(path)
		}
		return nil, err
	}
	return cfg, nil
}

func missingConfigError(path string) error {
	return fmt.Errorf("config file not found at %s; run `slick config init`", human.ContractHome(path))
}
