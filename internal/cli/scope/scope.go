package scope

import (
	"context"
	"errors"
	"strings"

	xstrings "github.com/gechr/x/strings"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	slackgo "github.com/slack-go/slack"
)

// Requirement specifies required Slack OAuth scopes. All must be present,
// or at least one of Any must be present.
type Requirement struct {
	All []string
	Any []string
}

// AllOf returns a Requirement where every listed scope must be present.
func AllOf(scopes ...string) Requirement {
	return Requirement{All: scopes}
}

// AnyOf returns a Requirement where at least one listed scope must be present.
func AnyOf(scopes ...string) Requirement {
	return Requirement{Any: scopes}
}

// Require checks that all requirements are satisfied by the token's scopes.
// It calls auth.test once and validates the X-OAuth-Scopes header.
func Require(ctx context.Context, client *slackgo.Client, requirements ...Requirement) error {
	if len(requirements) == 0 {
		return nil
	}
	auth, err := client.AuthTestContext(ctx)
	if err != nil {
		var slackErr slackgo.SlackErrorResponse
		if errors.As(err, &slackErr) && slackErr.Err == "method_not_found" {
			return nil
		}
		if strings.Contains(err.Error(), "slack server error: 404 Not Found") {
			return nil
		}
		return err
	}
	scopes := parseScopes(auth.Header.Get("X-OAuth-Scopes"))
	if len(scopes) == 0 {
		return nil
	}
	for _, req := range requirements {
		if err := validateRequirement(scopes, req); err != nil {
			return err
		}
	}
	return nil
}

func parseScopes(value string) map[string]bool {
	out := map[string]bool{}
	for _, s := range xstrings.SplitCSV(value) {
		out[s] = true
	}
	return out
}

func validateRequirement(scopes map[string]bool, req Requirement) error {
	var missing []string
	for _, s := range req.All {
		if !scopes[s] {
			missing = append(missing, s)
		}
	}
	if len(missing) > 0 {
		return clioutput.MissingScopeError{All: missing}
	}
	if len(req.Any) == 0 {
		return nil
	}
	for _, s := range req.Any {
		if scopes[s] {
			return nil
		}
	}
	return clioutput.MissingScopeError{Any: req.Any}
}
