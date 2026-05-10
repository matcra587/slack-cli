package main

import (
	"strings"
	"testing"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/spf13/cobra"
)

// TestCommandActionLabelCoverage walks the cobra command tree and asserts
// that every leaf command has a registered action label in
// clioutput.CommandActionLabels(). The plain-mode WritePlain methods rely
// on the registry to render past-tense action messages; a missing entry
// would surface as the dotted command id leaking into user-facing output.
func TestCommandActionLabelCoverage(t *testing.T) {
	root := NewRootCommand()
	labels := clioutput.CommandActionLabels()

	for _, leaf := range collectLeafCommandIDs(root) {
		if _, ok := labels[leaf]; !ok {
			t.Errorf("leaf command %q has no registered label in clioutput.commandActionLabel; add one", leaf)
		}
	}
}

// collectLeafCommandIDs returns the dotted ids of every visible leaf
// command reachable from root, excluding the root itself and any
// command marked Hidden (or whose ancestor is marked Hidden). Hidden
// commands cover cobra builtins (`completion`, `help`) that do not flow
// through the action-label rendering path.
func collectLeafCommandIDs(root *cobra.Command) []string {
	var ids []string
	var walk func(cmd *cobra.Command, path []string, hiddenAncestor bool)
	walk = func(cmd *cobra.Command, path []string, hiddenAncestor bool) {
		hidden := hiddenAncestor || cmd.Hidden
		children := cmd.Commands()
		if len(children) == 0 {
			if hidden || cmd == root {
				return
			}
			ids = append(ids, strings.Join(path, "."))
			return
		}
		for _, child := range children {
			walk(child, append(path, child.Name()), hidden)
		}
	}
	walk(root, nil, false)
	return ids
}
