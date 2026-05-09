package main

import (
	"context"

	cliscope "github.com/matcra587/slack-cli/internal/cli/scope"
	slackgo "github.com/slack-go/slack"
)

type scopeRequirement = cliscope.Requirement

func allScopes(scopes ...string) scopeRequirement { return cliscope.AllOf(scopes...) }

func requireSlackScopes(ctx context.Context, client *slackgo.Client, requirements ...scopeRequirement) error {
	return cliscope.Require(ctx, client, requirements...)
}
