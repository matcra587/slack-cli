package main

import (
	"path/filepath"
	"testing"
)

func TestFailureSelectorsKeepValuesClustersAndTerminatorsDistinct(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	for _, tc := range []struct {
		name              string
		args              []string
		output, workspace string
	}{
		{"defaults", []string{"cache", "users"}, "auto", ""},
		{"long selectors", []string{"cache", "users", "--output=compact", "--workspace=Other"}, "compact", "Other"},
		{"selector cluster", []string{"cache", "users", "-QwOther", "-Dohuman"}, "human", "Other"},
		{"short equals", []string{"cache", "users", "-o=json", "-w=Other"}, "json", "Other"},
		{"empty attached", []string{"cache", "users", "-wOther", "-w=", "-o="}, "", ""},
		{"empty separate", []string{"cache", "users", "-w", "", "-o", ""}, "", ""},
		{"missing later values", []string{"cache", "users", "-wOther", "--workspace"}, "auto", "Other"},
		{"message body", []string{"message", "send", "--message", "--workspace=Other", "--attribution-label", "-ohuman"}, "auto", ""},
		{"short message body", []string{"message", "send", "-m", "-wOther", "--output=compact"}, "compact", ""},
		{"literal terminator", []string{"cache", "users", "--", "-wOther", "--output=human"}, "auto", ""},
		{"repeated selectors", []string{"-wFirst", "cache", "users", "--workspace=Last", "-ojson", "-ocompact"}, "compact", "Last"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := NewRootCommand()
			if got := failureOutputMode(root, tc.args); got != tc.output {
				t.Fatalf("output selector=%q want=%q", got, tc.output)
			}
			if got := failureStringFlag(root, tc.args, "workspace", "w", ""); got != tc.workspace {
				t.Fatalf("workspace selector=%q want=%q", got, tc.workspace)
			}
		})
	}
}
