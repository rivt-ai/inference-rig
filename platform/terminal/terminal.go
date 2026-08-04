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
	return isTerminal(input) && isTerminal(output)
}

// IsWriterTerminal reports whether output alone is a terminal. Rendering only
// writes, so it must not care about stdin: `infr info < /dev/null` still has a
// human reading its stdout, and asking IsInteractive there would answer no and
// silently drop the colour.
func IsWriterTerminal(output io.Writer) bool {
	return isTerminal(output)
}

func isTerminal(stream any) bool {
	file, ok := stream.(interface{ Fd() uintptr })
	return ok && term.IsTerminal(int(file.Fd()))
}
