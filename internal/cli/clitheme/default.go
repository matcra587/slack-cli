package clitheme

import (
	"image/color"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gechr/clib/theme"
)

// Default resolves slick's theme without changing its established dark palette
// or the SLICK_THEME/CLIB_THEME precedence and preset aliases.
func Default() *theme.Theme {
	name := os.Getenv("SLICK_THEME")
	if name == "" {
		name = os.Getenv("CLIB_THEME")
	}
	name = strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(name)))
	switch name {
	case "plain", "monochrome", "solarized":
		name += "-dark"
	}
	var selected theme.Theme
	if name != "" && name != "default" && selected.UnmarshalText([]byte(name)) == nil {
		return &selected
	}
	th := theme.Dark()
	th.HelpDescBacktick = new(lipgloss.NewStyle().Foreground(lipgloss.Color("189")))
	th.HelpFlagExample = new(lipgloss.NewStyle().Foreground(lipgloss.Color("2")))
	th.EntityColors = []color.Color{
		lipgloss.Color("208"), lipgloss.Color("51"), lipgloss.Color("226"), lipgloss.Color("207"),
		lipgloss.Color("82"), lipgloss.Color("75"), lipgloss.Color("214"), lipgloss.Color("177"),
		lipgloss.Color("48"), lipgloss.Color("87"), lipgloss.Color("220"), lipgloss.Color("141"),
		lipgloss.Color("118"), lipgloss.Color("50"), lipgloss.Color("213"), lipgloss.Color("111"),
		lipgloss.Color("156"), lipgloss.Color("183"), lipgloss.Color("229"), lipgloss.Color("123"),
	}
	return th
}
