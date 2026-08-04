package cli

import (
	"strings"

	"github.com/antonikliment/tuikit"
)

// titleize turns a proto field name into a label, dropping the unit the
// rendered value already carries: without the trim, "15.6 GiB" sits under a
// column headed "SIZE BYTES".
//
// "_percent" is deliberately NOT trimmed the same way: used_memory_bytes has no
// sibling to collide with, but used_memory_percent does, and trimming would
// label both rows "Used memory".
func titleize(name string) string {
	return tuikit.Titleize(strings.TrimSuffix(strings.TrimSuffix(name, "_bytes"), "Bytes"))
}
