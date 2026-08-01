// Package prompt owns the look of InferenceRig's interactive terminal forms.
//
// It exists so the setup wizard and the diagnostic's repair picker are visibly
// the same application. A second copy of these styles would drift the moment
// either is touched.
package prompt

import (
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// Theme is the shared huh theme.
func Theme() huh.ThemeFunc { return styles }

func styles(isDark bool) *huh.Styles {
	teal := lipgloss.Color("14")
	s := huh.ThemeCharm(isDark)
	s.Focused.SelectSelector = s.Focused.SelectSelector.Foreground(teal)
	s.Focused.NextIndicator = s.Focused.NextIndicator.Foreground(teal)
	s.Focused.PrevIndicator = s.Focused.PrevIndicator.Foreground(teal)
	s.Focused.MultiSelectSelector = s.Focused.MultiSelectSelector.Foreground(teal)
	s.Focused.FocusedButton = s.Focused.FocusedButton.Background(teal)
	s.Focused.Next = s.Focused.FocusedButton
	s.Focused.TextInput.Prompt = s.Focused.TextInput.Prompt.Foreground(teal)
	s.Blurred = s.Focused
	s.Blurred.Base = s.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
	s.Blurred.Card = s.Blurred.Base
	s.Blurred.NextIndicator = lipgloss.NewStyle()
	s.Blurred.PrevIndicator = lipgloss.NewStyle()
	return s
}
