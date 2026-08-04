package cli

import (
	"strings"
	"unicode"
)

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
