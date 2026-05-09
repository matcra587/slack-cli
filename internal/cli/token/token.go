package token

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/config"
	slackgo "github.com/slack-go/slack"
)

var (
	_ cliruntime.TokenResolver = EnvTokenResolver{}
	_ cliruntime.TokenResolver = CredentialTokenResolver{}
)

type EnvTokenResolver struct{}

func (EnvTokenResolver) ResolveToken(_ context.Context, profile config.WorkspaceProfile) (string, error) {
	if after, ok := strings.CutPrefix(profile.TokenRef, "env:"); ok {
		name := after
		token := os.Getenv(name)
		if token == "" {
			return "", errors.New("token environment variable is empty")
		}
		return token, nil
	}
	return "", errors.New("unsupported token reference")
}

type CredentialTokenResolver struct {
	Store        config.CredentialStore
	SlackBaseURL string
	HTTPClient   *http.Client
	Now          func() time.Time
	SecretReader SecretReader
}

type SecretReader interface {
	Read(ctx context.Context, ref string) (string, error)
}

// SecretReaderFunc adapts a function to SecretReader. Used by tests to inject
// fakes; production code uses opSecretReader.
type SecretReaderFunc func(ctx context.Context, ref string) (string, error)

func (f SecretReaderFunc) Read(ctx context.Context, ref string) (string, error) { return f(ctx, ref) }

type opSecretReader struct{}

func (opSecretReader) Read(ctx context.Context, ref string) (string, error) {
	out, err := exec.CommandContext(ctx, "op", "read", ref).Output() //nolint:gosec // The 1Password reference is explicit user configuration, not shell-expanded input.
	if err != nil {
		return "", fmt.Errorf("reading 1Password secret: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (r CredentialTokenResolver) secretReader() SecretReader {
	if r.SecretReader != nil {
		return r.SecretReader
	}
	return opSecretReader{}
}

func (r CredentialTokenResolver) ResolveToken(ctx context.Context, profile config.WorkspaceProfile) (string, error) {
	if token, ok := RuntimeEnvToken(profile.Name); ok {
		return token, nil
	}
	if strings.TrimSpace(profile.TokenRef) == "" {
		return "", config.ErrCredentialNotFound
	}
	if strings.HasPrefix(profile.TokenRef, "env:") {
		return EnvTokenResolver{}.ResolveToken(ctx, profile)
	}
	if strings.HasPrefix(profile.TokenRef, "keychain:slack-cli/") {
		if r.Store == nil {
			return "", errors.New("credential store is unavailable")
		}
		name := strings.TrimPrefix(profile.TokenRef, "keychain:slack-cli/")
		secret, err := r.Store.Get("slack-cli", name)
		if err != nil {
			return "", err
		}
		return r.resolveStoredCredential(ctx, name, secret)
	}
	if strings.HasPrefix(profile.TokenRef, "op://") {
		secret, err := r.secretReader().Read(ctx, profile.TokenRef)
		if err != nil {
			return "", err
		}
		return accessTokenFromStructuredSecret(secret)
	}
	return "", errors.New("unsupported token reference")
}

func RuntimeEnvToken(profileName string) (string, bool) {
	for _, name := range runtimeTokenEnvNames(profileName) {
		if token := strings.TrimSpace(os.Getenv(name)); token != "" {
			return token, true
		}
	}
	return "", false
}

func runtimeTokenEnvNames(profileName string) []string {
	names := make([]string, 0, 2)
	if normalized := normalizeProfileEnvSuffix(profileName); normalized != "" {
		names = append(names, "SLACK_CLI_TOKEN_"+normalized)
	}
	names = append(names, "SLACK_CLI_TOKEN")
	return names
}

func normalizeProfileEnvSuffix(profileName string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(profileName)) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	return strings.Trim(b.String(), "_")
}

const credentialRefreshBuffer = 5 * time.Minute

func (r CredentialTokenResolver) resolveStoredCredential(ctx context.Context, name, secret string) (string, error) {
	credential, err := config.DecodeCredential(secret)
	if err != nil {
		return "", err
	}
	if !r.credentialNeedsRefresh(credential) {
		return credential.AccessToken, nil
	}
	if credential.ClientID == "" || credential.RefreshToken == "" {
		return "", errors.New("credential is expiring and cannot be refreshed")
	}
	response, err := oauthRefreshToken(ctx, r.oauthHTTPClient(), r.SlackBaseURL, credential.ClientID, credential.RefreshToken)
	if err != nil {
		return "", err
	}
	updated := credentialPayloadFromOAuthRefresh(credential.ClientID, r.now(), response)
	encoded, err := config.EncodeCredential(updated)
	if err != nil {
		return "", err
	}
	if r.Store != nil && name != "" {
		if err := r.Store.Set("slack-cli", name, encoded); err != nil {
			return "", err
		}
	}
	return updated.AccessToken, nil
}

func (r CredentialTokenResolver) credentialNeedsRefresh(credential config.CredentialPayload) bool {
	if credential.ExpiresAt == nil {
		return false
	}
	return !r.now().Before(credential.ExpiresAt.Add(-credentialRefreshBuffer))
}

func (r CredentialTokenResolver) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r CredentialTokenResolver) oauthHTTPClient() *http.Client {
	if r.HTTPClient != nil {
		return r.HTTPClient
	}
	return &http.Client{}
}

func credentialPayloadFromOAuthRefresh(clientID string, now time.Time, response *slackgo.OAuthV2Response) config.CredentialPayload {
	payload := config.CredentialPayload{ClientID: clientID}
	if response.AuthedUser.AccessToken != "" {
		payload.AccessToken = response.AuthedUser.AccessToken
		payload.RefreshToken = response.AuthedUser.RefreshToken
		if response.AuthedUser.ExpiresIn > 0 {
			expiresAt := now.Add(time.Duration(response.AuthedUser.ExpiresIn) * time.Second)
			payload.ExpiresAt = &expiresAt
		}
		return payload
	}
	payload.AccessToken = response.AccessToken
	payload.RefreshToken = response.RefreshToken
	if response.ExpiresIn > 0 {
		expiresAt := now.Add(time.Duration(response.ExpiresIn) * time.Second)
		payload.ExpiresAt = &expiresAt
	}
	return payload
}

func accessTokenFromStructuredSecret(secret string) (string, error) {
	credential, err := config.DecodeCredential(secret)
	if err != nil {
		return "", err
	}
	return credential.AccessToken, nil
}

func oauthRefreshToken(ctx context.Context, httpClient *http.Client, baseURL, clientID, refreshToken string) (*slackgo.OAuthV2Response, error) {
	var opts []slackgo.OAuthOption
	if baseURL != "" {
		opts = append(opts, slackgo.OAuthOptionAPIURL(slackAPIURL(baseURL)))
	}
	resp, err := slackgo.RefreshOAuthV2TokenContext(ctx, httpClient, clientID, "", refreshToken, opts...)
	if err != nil {
		return nil, wrapBadClientSecret(err)
	}
	return resp, nil
}

// slackAPIURL mirrors cmd/slick/slack_native.go:slackAPIURL; collapse when the
// auth/slack-api packages emerge.
func slackAPIURL(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(baseURL, "/api") {
		return baseURL + "/"
	}
	return baseURL + "/api/"
}

// wrapBadClientSecret mirrors cmd/slick/auth.go:wrapBadClientSecret; collapse
// when the auth package emerges.
func wrapBadClientSecret(err error) error {
	var slackErr slackgo.SlackErrorResponse
	if errors.As(err, &slackErr) && slackErr.Err == "bad_client_secret" {
		return fmt.Errorf("bad_client_secret: Slack treated this as a client-secret OAuth flow. Enable PKCE for the Slack app, or import a manifest with oauth_config.pkce_enabled=true; slack-cli local OAuth intentionally omits the client secret: %w", err)
	}
	return err
}
