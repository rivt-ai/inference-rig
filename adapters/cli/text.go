package cli

import (
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
)

// indentUnit is two spaces per level. Wide enough to see, narrow enough that a
// nested message plus a table still fits an 80-column terminal.
const indentUnit = "  "

func indent(text string, depth int) string {
	return strings.Repeat(indentUnit, depth) + text
}

func indentLines(text string, depth int) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		b.WriteString(indent(line, depth) + "\n")
	}
	return b.String()
}

// titleize turns a proto field name into a label: "running_profiles" and its
// JSON spelling "runningProfiles" both become "Running profiles". Sentence
// case, not Title Case, so "Autostart profiles" does not read as a proper noun.
// The unit is already in the rendered value ("15.6 GiB"), so carrying it in
// the label too gives "SIZE BYTES  15.6 GiB".
func titleize(name string) string {
	name = strings.TrimSuffix(strings.TrimSuffix(name, "_bytes"), "Bytes")
	// "_percent" is deliberately NOT trimmed the same way: used_memory_bytes
	// has no sibling to collide with, but used_memory_percent does, and
	// trimming would label both rows "Used memory".
	var b strings.Builder
	for i, r := range name {
		switch {
		case r == '_':
			b.WriteRune(' ')
		case unicode.IsUpper(r) && i > 0:
			b.WriteRune(' ')
			b.WriteRune(unicode.ToLower(r))
		case i == 0:
			b.WriteRune(unicode.ToUpper(r))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// columnWidths measures each column by its widest cell. Width is measured with
// lipgloss.Width rather than len, because a coloured cell carries ANSI escapes
// that occupy no screen columns; using len would pad by the length of the
// escape sequence and skew the whole table right.
func columnWidths(rows [][]string) []int {
	var widths []int
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], lipgloss.Width(cell))
		}
	}
	return widths
}

// joinCells lays a row out against widths. The final column is not padded, so
// no line carries trailing whitespace.
func joinCells(row []string, widths []int) string {
	var b strings.Builder
	for i, cell := range row {
		if i == len(row)-1 {
			b.WriteString(cell)
			break
		}
		b.WriteString(cell + strings.Repeat(" ", widths[i]-lipgloss.Width(cell)+2))
	}
	return strings.TrimRight(b.String(), " ")
}

func pad(text string, width int) string {
	return text + strings.Repeat(" ", max(0, width-lipgloss.Width(text)))
}

func longest(values []string) int {
	width := 0
	for _, value := range values {
		width = max(width, lipgloss.Width(value))
	}
	return width
}
