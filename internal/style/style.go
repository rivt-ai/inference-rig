// Package style owns the one colour palette the whole binary renders with.
//
// It exists because the TUI and the CLI must not drift: a "running" runtime
// should be the same green in `infr runtime status` as it is in the TUI's
// profile pane. Both import this; neither defines a colour of its own.
package style

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/antonikliment/tuikit"
)

// The ANSI-16 palette. Numbered rather than hex so it inherits whatever the
// user's terminal theme defines, which is what makes the output legible on
// both light and dark backgrounds without us guessing.
var (
	Green  color.Color = lipgloss.Color("10")
	Blue   color.Color = lipgloss.Color("12")
	Yellow color.Color = lipgloss.Color("11")
	Red    color.Color = lipgloss.Color("9")
	Cyan   color.Color = lipgloss.Color("14")
	Muted  color.Color = lipgloss.Color("8")
)

// Theme is the tuikit theme every panel, tab and status line is built from.
var Theme = tuikit.Theme{
	Green: Green, Blue: Blue, Yellow: Yellow, Red: Red, Cyan: Cyan,
	Muted: Muted, Brand: lipgloss.Color("63"), TabActiveFg: lipgloss.Color("0"),
	FocusBorder: Yellow,
}

var (
	MutedStyle    = lipgloss.NewStyle().Foreground(Muted)
	SuccessStyle  = lipgloss.NewStyle().Foreground(Green)
	WarningStyle  = lipgloss.NewStyle().Foreground(Yellow)
	ErrorStyle    = lipgloss.NewStyle().Foreground(Red)
	SelectedStyle = lipgloss.NewStyle().Background(Cyan).Foreground(lipgloss.Color("0"))
	// LinkStyle marks a URL the user is meant to click or copy. Underlined as
	// well as coloured, so it still reads as a link where colour is lost.
	LinkStyle = lipgloss.NewStyle().Foreground(Cyan).Underline(true)
)

// The terminal-vs-pipe half of rendering now lives in tuikit, which owns the
// same question for its own components. These names stay because they are what
// this binary's call sites read as — Interactive is the predicate that decides
// styled-vs-machine output, and Colour is deliberately a different question:
// NO_COLOR says "do not paint", not "change format", and conflating the two
// hands a user who set it the JSON they did not ask for. See https://no-color.org.
type Painter = tuikit.Painter

var (
	Plain       = tuikit.Plain
	Paint       = tuikit.Paint
	PainterFor  = tuikit.PainterFor
	Interactive = tuikit.IsTerminalWriter
	Colour      = tuikit.ColorEnabled
)
