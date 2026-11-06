package main

import (
	"errors"

	"github.com/gechr/clib/complete"
	"github.com/spf13/cobra"
)

// Completion actions belong to the root invocation, including generated
// positional context after --. Never silently run a selected Slack command.
func validateCompletionPlacement(cmd *cobra.Command) error {
	if cmd == cmd.Root() {
		return nil
	}
	flags := cmd.Root().PersistentFlags()
	active, _ := flags.GetString(complete.FlagComplete)
	if active != "" {
		return errors.New("completion actions must be invoked on slick without a subcommand")
	}
	for _, name := range []string{complete.FlagInstallCompletion, complete.FlagUninstallCompletion, complete.FlagPrintCompletion} {
		enabled, _ := flags.GetBool(name)
		if enabled {
			return errors.New("completion actions must be invoked on slick without a subcommand")
		}
	}
	return nil
}
