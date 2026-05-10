// Package clitheme provides shared UI styling helpers used by interactive
// CLI commands. The huh form theme bridges clib's semantic palette into
// huh.Theme so auth and config flows render with consistent styling.
package clitheme

import (
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
		helpCommand := derefStyle(resolved.HelpCommand)
		helpDim := derefStyle(resolved.HelpDim)
		helpPlaceholder := derefStyle(resolved.HelpValuePlaceholder)
		red := derefStyle(resolved.Red)

		t.Focused.Title = helpCommand
		t.Focused.NoteTitle = helpCommand
		t.Focused.Description = helpDim
		t.Focused.ErrorIndicator = mergeLoginHuhStyle(t.Focused.ErrorIndicator, red)
		t.Focused.ErrorMessage = red
		t.Focused.TextInput.Cursor = mergeLoginHuhStyle(t.Focused.TextInput.Cursor, helpCommand)
		t.Focused.TextInput.Placeholder = helpPlaceholder
		t.Focused.TextInput.Prompt = helpCommand
		// Buttons: focused (the selected affirmative/negative) gets the
		// theme's command color as a solid background; blurred buttons sit
		// quietly in dim. Inherit huh.ThemeBase's button padding/margin so
		// only the colors change.
		if helpCommandFg := helpCommand.GetForeground(); helpCommandFg != nil {
			t.Focused.FocusedButton = t.Focused.FocusedButton.
				Foreground(lipgloss.Color("0")).
				Background(helpCommandFg).
				Bold(true)
		}
		if helpDimFg := helpDim.GetForeground(); helpDimFg != nil {
			t.Focused.BlurredButton = t.Focused.BlurredButton.
				Foreground(helpDimFg).
				UnsetBackground()
		} else {
			t.Focused.BlurredButton = t.Focused.BlurredButton.Faint(true).UnsetBackground()
		}

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

func derefStyle(s *lipgloss.Style) lipgloss.Style {
	if s == nil {
		return lipgloss.NewStyle()
	}
	return *s
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
