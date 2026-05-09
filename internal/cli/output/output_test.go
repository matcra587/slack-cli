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
		{name: "json flag wins", flags: clioutput.OutputFlags{JSON: true}, tty: true, want: clioutput.RenderModeEnvelope},
		{name: "plain flag wins", flags: clioutput.OutputFlags{Plain: true}, agent: true, want: clioutput.RenderModePlain},
		{name: "compact flag wins", flags: clioutput.OutputFlags{Compact: true}, tty: true, want: clioutput.RenderModeCompact},
		{name: "raw flag wins", flags: clioutput.OutputFlags{Raw: true}, want: clioutput.RenderModeRaw},
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
