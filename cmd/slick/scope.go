package main

import (
	"context"
	"errors"
	"strings"

	xstrings "github.com/gechr/x/strings"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	slackgo "github.com/slack-go/slack"
)

type scopeRequirement struct {
	all []string
	any []string
}

func allScopes(scopes ...string) scopeRequirement {
	return scopeRequirement{all: scopes}
}

func anyScope(scopes ...string) scopeRequirement {
	return scopeRequirement{any: scopes}
}

func requireSlackScopes(ctx context.Context, client *slackgo.Client, requirements ...scopeRequirement) error {
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
	scopes := parseSlackScopes(auth.Header.Get("X-OAuth-Scopes"))
	if len(scopes) == 0 {
		return nil
	}
	for _, requirement := range requirements {
		if err := validateScopeRequirement(scopes, requirement); err != nil {
			return err
		}
	}
	return nil
}

func parseSlackScopes(value string) map[string]bool {
	out := map[string]bool{}
	for _, scope := range xstrings.SplitCSV(value) {
		out[scope] = true
	}
	return out
}

func validateScopeRequirement(scopes map[string]bool, requirement scopeRequirement) error {
	var missing []string
	for _, scope := range requirement.all {
		if !scopes[scope] {
			missing = append(missing, scope)
		}
	}
	if len(missing) > 0 {
		return clioutput.MissingScopeError{All: missing}
	}
	if len(requirement.any) == 0 {
		return nil
	}
	for _, scope := range requirement.any {
		if scopes[scope] {
			return nil
		}
	}
	return clioutput.MissingScopeError{Any: requirement.any}
}
