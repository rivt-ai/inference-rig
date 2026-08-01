// Package terminal answers whether a stream is attached to a terminal.
//
// It exists so the one predicate that decides "may this prompt" has a single
// owner. A command that prompts without checking hangs forever in CI instead
// of failing with something an operator can act on.
package terminal

import (
	"io"

	"golang.org/x/term"
)

// IsInteractive reports whether both streams are terminals. Both, because a
// prompt writes to one and reads from the other: redirecting either leaves
// nobody to answer it.
func IsInteractive(input io.Reader, output io.Writer) bool {
	in, inOK := input.(interface{ Fd() uintptr })
	out, outOK := output.(interface{ Fd() uintptr })
	return inOK && outOK && term.IsTerminal(int(in.Fd())) && term.IsTerminal(int(out.Fd()))
}
