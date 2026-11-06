// Package token preserves the CLI's credential resolver names while the shared
// implementation lives in internal/credentials.
package token

import (
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/credentials"
)

var (
	_ cliruntime.TokenResolver = EnvTokenResolver{}
	_ cliruntime.TokenResolver = CredentialTokenResolver{}
)

type EnvTokenResolver = credentials.EnvTokenResolver

type CredentialTokenResolver = credentials.CredentialTokenResolver

type SecretReader = credentials.SecretReader

type SecretReaderFunc = credentials.SecretReaderFunc

func RuntimeEnvToken(profileName string) (string, bool) {
	return credentials.RuntimeEnvToken(profileName)
}
