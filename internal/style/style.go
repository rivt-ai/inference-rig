// Package style owns the one colour palette the whole binary renders with.
//
// It exists because the TUI and the CLI must not drift: a "running" runtime
// should be the same green in `infr runtime status` as it is in the TUI's
// profile pane. Both import this; neither defines a colour of its own.
package style

import (
	"image/color"
	"io"
	"os"

	"charm.land/lipgloss/v2"
	"github.com/antonikliment/tuikit"

	"inferencerig/platform/terminal"
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

// Interactive reports whether w is a terminal a human is watching. It is the
// predicate that decides styled-vs-machine output, and it delegates the TTY
// half to platform/terminal so there is still only one owner of that question.
//
// NO_COLOR is honoured because a user who set it has already told every other
// tool on their machine what they want; see https://no-color.org.
func Interactive(w io.Writer) bool {
	return terminal.IsWriterTerminal(w)
}

// Painter applies a style to text, or returns it untouched when the
// destination cannot take escapes.
//
// Passing one of these around is what stops every call site from repeating the
// "is this a terminal, and is NO_COLOR set" test — and from getting it subtly
// different, which is how half a command's output ends up coloured.
type Painter func(lipgloss.Style, string) string

// Plain is the Painter that never paints. Tests use it to assert on content
// without decoding escape sequences.
func Plain(_ lipgloss.Style, text string) string { return text }

// Paint is the Painter that always paints, for output already known to be
// going to a terminal.
func Paint(s lipgloss.Style, text string) string { return s.Render(text) }

// PainterFor returns the Painter appropriate to w, deciding once so the answer
// cannot change halfway through rendering one command's output.
func PainterFor(w io.Writer) Painter {
	if !Colour(w) {
		return Plain
	}
	return Paint
}

// Colour reports whether output to w may carry ANSI escapes.
//
// It is a separate question from Interactive on purpose. NO_COLOR says "do not
// paint", not "change format": a user who sets it still wants the readable
// table, just without the escapes. Conflating the two hands them JSON, which
// is the opposite of what they asked for. See https://no-color.org.
func Colour(w io.Writer) bool {
	if _, set := os.LookupEnv("NO_COLOR"); set {
		return false
	}
	return Interactive(w)
}
