package tui

import (
	"image/color"

	"inferencerig/internal/style"
)

// The palette lives in internal/style so the CLI renders in the same colours.
// These aliases keep the TUI's call sites unchanged; they are not a second
// definition.
var (
	green  = style.Green
	blue   = style.Blue
	yellow = style.Yellow
	red    = style.Red
	cyan   = style.Cyan
	muted  = style.Muted

	theme         = style.Theme
	mutedStyle    = style.MutedStyle
	successStyle  = style.SuccessStyle
	warningStyle  = style.WarningStyle
	errorStyle    = style.ErrorStyle
	selectedStyle = style.SelectedStyle
)

func panel(accent color.Color, focused bool, width, height int, content string) string {
	return theme.PanelStyle(accent, focused).Width(width).Height(height).Render(content)
}
