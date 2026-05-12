package output_test

import (
	"testing"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
)

func TestOutputModeSelection(t *testing.T) {
	tests := []struct {
		name  string
		flags clioutput.OutputFlags
		tty   bool
		agent bool
		want  clioutput.RenderMode
	}{
		{name: "tty defaults to plain", tty: true, want: clioutput.RenderModePlain},
		{name: "non tty defaults to json", want: clioutput.RenderModeEnvelope},
		{name: "agent defaults to json", tty: true, agent: true, want: clioutput.RenderModeEnvelope},
		{name: "json wins", flags: clioutput.OutputFlags{Output: clioutput.OutputJSON}, tty: true, want: clioutput.RenderModeEnvelope},
		{name: "human wins", flags: clioutput.OutputFlags{Output: clioutput.OutputHuman}, agent: true, want: clioutput.RenderModePlain},
		{name: "compact wins", flags: clioutput.OutputFlags{Output: clioutput.OutputCompact}, tty: true, want: clioutput.RenderModeCompact},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.flags.Resolve(tt.tty, tt.agent)
			if got != tt.want {
				t.Fatalf("Resolve() = %v, want %v", got, tt.want)
			}
		})
	}
}
