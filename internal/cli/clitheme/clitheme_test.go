package clitheme

import (
	"fmt"
	"image/color"
	"testing"

	"charm.land/lipgloss/v2"
	clibtheme "github.com/gechr/clib/theme"
)

func TestLoginHuhThemeUsesClibSemanticColors(t *testing.T) {
	th := Default().With(
		clibtheme.WithHelpCommand(lipgloss.NewStyle().Foreground(lipgloss.Color("#123456"))),
		clibtheme.WithHelpDim(lipgloss.NewStyle().Foreground(lipgloss.Color("#654321"))),
		clibtheme.WithHelpFlag(lipgloss.NewStyle().Foreground(lipgloss.Color("#fedcba"))),
		clibtheme.WithHelpPlaceholder(lipgloss.NewStyle().Foreground(lipgloss.Color("#abcdef"))),
	)

	got := LoginHuhTheme(th).Theme(false)
	assertSameColor(t, "#123456", got.Focused.Title.GetForeground())
	assertSameColor(t, "#654321", got.Focused.Description.GetForeground())
	assertSameColor(t, "#123456", got.Focused.TextInput.Prompt.GetForeground())
	assertSameColor(t, "#abcdef", got.Focused.TextInput.Placeholder.GetForeground())
}

func assertSameColor(t *testing.T, want string, got color.Color) {
	t.Helper()
	r, g, b, _ := got.RGBA()
	gotHex := fmt.Sprintf("#%02x%02x%02x", uint8(r>>8), uint8(g>>8), uint8(b>>8))
	if gotHex != want {
		t.Fatalf("color = %s, want %s", gotHex, want)
	}
}

func TestMergeLoginHuhStylePreservesUnsetColors(t *testing.T) {
	base := lipgloss.NewStyle().Foreground(lipgloss.Color("#123456")).Background(lipgloss.Color("#654321"))
	got := mergeLoginHuhStyle(base, lipgloss.NewStyle().Bold(true))
	assertSameColor(t, "#123456", got.GetForeground())
	assertSameColor(t, "#654321", got.GetBackground())
	if !got.GetBold() {
		t.Fatal("bold override was lost")
	}
}
