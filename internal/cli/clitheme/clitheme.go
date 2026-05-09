// Package clitheme provides shared UI styling helpers used by interactive
// CLI commands. The huh form theme bridges clib's semantic palette into
// huh.Theme so auth and config flows render with consistent styling.
package clitheme

import (
	"fmt"
	"image/color"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	clibtheme "github.com/gechr/clib/theme"
)

// LoginHuhTheme returns the huh form theme used for login and config forms.
// Falls back to the default clib theme if th is nil; falls back to the huh
// base theme for plain or monochrome themes.
func LoginHuhTheme(th *clibtheme.Theme) huh.Theme {
	if th == nil {
		th = clibtheme.Default()
	}
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		if th.String() == "plain" || th.String() == "monochrome" {
			return huh.ThemeBase(isDark)
		}
		resolved := th.Init()

		t := huh.ThemeBase(isDark)
		helpCommand := loginHuhStyle(resolved.HelpCommand)
		helpDim := loginHuhStyle(resolved.HelpDim)
		helpPlaceholder := loginHuhStyle(resolved.HelpValuePlaceholder)
		red := loginHuhStyle(resolved.Red)

		t.Focused.Title = helpCommand
		t.Focused.NoteTitle = helpCommand
		t.Focused.Description = helpDim
		t.Focused.ErrorIndicator = mergeLoginHuhStyle(t.Focused.ErrorIndicator, red)
		t.Focused.ErrorMessage = red
		t.Focused.TextInput.Cursor = mergeLoginHuhStyle(t.Focused.TextInput.Cursor, helpCommand)
		t.Focused.TextInput.Placeholder = helpPlaceholder
		t.Focused.TextInput.Prompt = helpCommand

		t.Blurred = t.Focused
		t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
		t.Blurred.Card = t.Blurred.Base
		t.Blurred.NextIndicator = lipgloss.NewStyle()
		t.Blurred.PrevIndicator = lipgloss.NewStyle()

		t.Group.Title = t.Focused.Title
		t.Group.Description = t.Focused.Description
		return t
	})
}

func mergeLoginHuhStyle(base, override lipgloss.Style) lipgloss.Style {
	if foreground := override.GetForeground(); foreground != nil {
		base = base.Foreground(foreground)
	}
	if background := override.GetBackground(); background != nil {
		base = base.Background(background)
	}
	if override.GetBold() {
		base = base.Bold(true)
	}
	if override.GetFaint() {
		base = base.Faint(true)
	}
	if override.GetItalic() {
		base = base.Italic(true)
	}
	if override.GetUnderline() {
		base = base.Underline(true)
	}
	return base
}

func loginHuhStyle(style *lipgloss.Style) lipgloss.Style {
	if style == nil {
		return lipgloss.NewStyle()
	}
	converted := lipgloss.NewStyle()
	if style.GetBold() {
		converted = converted.Bold(true)
	}
	if style.GetFaint() {
		converted = converted.Faint(true)
	}
	if style.GetItalic() {
		converted = converted.Italic(true)
	}
	if style.GetUnderline() {
		converted = converted.Underline(true)
	}
	if foreground := LoginHuhColor(style.GetForeground()); foreground != nil {
		converted = converted.Foreground(foreground)
	}
	if background := LoginHuhColor(style.GetBackground()); background != nil {
		converted = converted.Background(background)
	}
	return converted
}

const colorComponentShift = 8

// LoginHuhColor converts a generic image/color.Color into a hex lipgloss
// color, returning nil for nil or NoColor inputs. Exported for tests that
// verify clib semantic colors flow through unchanged.
func LoginHuhColor(c color.Color) color.Color {
	if c == nil {
		return nil
	}
	if _, ok := c.(lipgloss.NoColor); ok {
		return nil
	}
	r, g, b, _ := c.RGBA()
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", uint8(r>>colorComponentShift), uint8(g>>colorComponentShift), uint8(b>>colorComponentShift)))
}
