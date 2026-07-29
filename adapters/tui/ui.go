package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/antonikliment/tuikit"
)

var (
	green  = lipgloss.Color("10")
	blue   = lipgloss.Color("12")
	yellow = lipgloss.Color("11")
	red    = lipgloss.Color("9")
	cyan   = lipgloss.Color("14")
	muted  = lipgloss.Color("8")

	theme = tuikit.Theme{
		Green: green, Blue: blue, Yellow: yellow, Red: red, Cyan: cyan,
		Muted: muted, Brand: lipgloss.Color("63"), TabActiveFg: lipgloss.Color("0"),
		FocusBorder: yellow,
	}
	mutedStyle    = lipgloss.NewStyle().Foreground(muted)
	successStyle  = lipgloss.NewStyle().Foreground(green)
	warningStyle  = lipgloss.NewStyle().Foreground(yellow)
	errorStyle    = lipgloss.NewStyle().Foreground(red)
	selectedStyle = lipgloss.NewStyle().Background(cyan).Foreground(lipgloss.Color("0"))
)

func panel(accent color.Color, focused bool, width, height int, content string) string {
	return theme.PanelStyle(accent, focused).Width(width).Height(height).Render(content)
}
