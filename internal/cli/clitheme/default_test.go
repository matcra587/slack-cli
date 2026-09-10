package clitheme

import (
	"testing"

	"charm.land/lipgloss/v2"
)

func TestDefaultThemeEnvironmentCompatibility(t *testing.T) {
	for _, tt := range []struct{ slick, clib, want string }{
		{"", "", "dark"},
		{"", "nord", "nord"},
		{"dracula", "nord", "dracula"},
		{"invalid", "nord", "dark"},
		{"default", "nord", "dark"},
		{"plain", "", "plain-dark"},
		{"monochrome", "", "monochrome-dark"},
		{"solarized", "", "solarized-dark"},
		{"plain-light", "", "plain-light"},
	} {
		t.Run(tt.slick+"/"+tt.clib, func(t *testing.T) {
			t.Setenv("SLICK_THEME", tt.slick)
			t.Setenv("CLIB_THEME", tt.clib)
			if got := Default().String(); got != tt.want {
				t.Fatalf("theme = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultThemePreservesPalette(t *testing.T) {
	t.Setenv("SLICK_THEME", "")
	t.Setenv("CLIB_THEME", "")
	th := Default()
	if th.HelpDescBacktick.GetForeground() != lipgloss.Color("189") ||
		th.HelpFlagExample.GetForeground() != lipgloss.Color("2") || th.HelpFlagExample.GetFaint() {
		t.Fatal("default help colors changed")
	}
	want := []string{
		"208", "51", "226", "207", "82", "75", "214", "177", "48", "87",
		"220", "141", "118", "50", "213", "111", "156", "183", "229", "123",
	}
	if len(th.EntityColors) != len(want) {
		t.Fatalf("entity colors = %d, want %d", len(th.EntityColors), len(want))
	}
	for i, c := range want {
		if th.EntityColors[i] != lipgloss.Color(c) {
			t.Fatalf("entity color %d changed", i)
		}
	}
}
