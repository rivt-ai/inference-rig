package llamacpp

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// ErrInvalidINI marks content that violates the models.ini grammar or key rules.
var ErrInvalidINI = errors.New("invalid models.ini")

// generatedHeader identifies every materialized models.ini and explains the
// lifetime of manual changes, followed by the file-level version key.
const generatedHeader = "; InferenceRig materialized runtime file.\n" +
	"; Edits inside a profile's own section are imported back into that profile's\n" +
	"; canonical YAML on the next materialization, leaving the rest of that\n" +
	"; profile.yaml — comments included — exactly as it is. Where the YAML changed\n" +
	"; too, the YAML wins and the edit is reported. Edits to [*], to a section\n" +
	"; naming no profile, and comments and ordering here are not imported.\n" +
	"version = 1\n"

// globalSection is the models.ini cascade section name that holds backend-wide
// defaults applied to every model.
const globalSection = "*"

// section is one models.ini stanza: a section name and its canonical key/values.
type section struct {
	Name   string
	Values map[string]string
}

// renderModelsINI produces the full, deterministic models.ini text: the
// generated header + version, then the backend-wide defaults as [*], then each
// named section. Sections are ordered by name and keys within a section are
// sorted, so identical input always renders byte-identical output. It validates
// every section name and key/value and returns ErrInvalidINI on any violation,
// so a caller can render-then-write and never replace a valid file with bad
// input.
func renderModelsINI(defaults map[string]string, sections []section) (string, error) {
	var out strings.Builder
	out.WriteString(generatedHeader)
	if len(defaults) > 0 {
		block, err := renderSection(section{Name: globalSection, Values: defaults})
		if err != nil {
			return "", err
		}
		out.WriteString(block)
	}
	ordered := slices.Clone(sections)
	slices.SortFunc(ordered, func(a, b section) int { return cmp.Compare(a.Name, b.Name) })
	seen := map[string]struct{}{}
	for _, s := range ordered {
		if s.Name == globalSection {
			return "", fmt.Errorf("%w: named section may not be %q", ErrInvalidINI, globalSection)
		}
		if _, dup := seen[s.Name]; dup {
			return "", fmt.Errorf("%w: duplicate section %q", ErrInvalidINI, s.Name)
		}
		seen[s.Name] = struct{}{}
		block, err := renderSection(s)
		if err != nil {
			return "", err
		}
		out.WriteString(block)
	}
	return out.String(), nil
}

// renderSection renders one stanza with canonical, deduplicated, sorted keys.
func renderSection(s section) (string, error) {
	if err := validateSectionName(s.Name); err != nil {
		return "", err
	}
	values, err := canonicalizeValues(s.Values)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	out.WriteString("\n[" + s.Name + "]\n")
	for _, key := range slices.Sorted(maps.Keys(values)) {
		fmt.Fprintf(&out, "%s = %s\n", key, values[key])
	}
	return out.String(), nil
}

// canonicalizeValues canonicalizes keys and rejects duplicates and newline-
// bearing values (which would inject new keys or sections).
func canonicalizeValues(raw map[string]string) (map[string]string, error) {
	values := make(map[string]string, len(raw))
	for key, value := range raw {
		canonical, err := canonicalKey(key)
		if err != nil {
			return nil, err
		}
		if _, exists := values[canonical]; exists {
			return nil, fmt.Errorf("%w: duplicate key %q", ErrInvalidINI, canonical)
		}
		if strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("%w: value for key %q contains a newline", ErrInvalidINI, key)
		}
		values[canonical] = value
	}
	return values, nil
}

func validateSectionName(name string) error {
	if strings.TrimSpace(name) == "" || strings.ContainsAny(name, "[]\r\n;#=") {
		return fmt.Errorf("%w: section name %q", ErrInvalidINI, name)
	}
	return nil
}

// keyAliases maps legacy/alternate models.ini keys to their canonical CLI form.
var keyAliases = map[string]string{
	"batch":            "batch-size",
	"ubatch":           "ubatch-size",
	"n-parallel":       "parallel",
	"endpoint-metrics": "metrics",
	"endpoint-props":   "props",
	"endpoint-slots":   "slots",
	"think":            "reasoning-format",
	"think-budget":     "reasoning-budget",
	"models_dir":       "models-dir",
	"models_preset":    "models-preset",
	"models_max":       "models-max",
}

var canonicalKeyPattern = regexp.MustCompile(`^[a-z_][a-z0-9_.-]*$`)
var inlineCommentPattern = regexp.MustCompile(`[ \t][;#]`)

// canonicalKey normalizes a models.ini key to its canonical lowercase CLI form.
// An environment-style LLAMA_ARG_* key is accepted only in exact uppercase and
// folded to the CLI form; any other mixed case is rejected.
func canonicalKey(key string) (string, error) {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return "", fmt.Errorf("%w: empty key", ErrInvalidINI)
	}
	if strings.HasPrefix(trimmed, "LLAMA_ARG_") {
		if trimmed != strings.ToUpper(trimmed) {
			return "", fmt.Errorf("%w: environment key %q must be uppercase", ErrInvalidINI, key)
		}
		trimmed = strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(trimmed, "LLAMA_ARG_"), "_", "-"))
	} else if trimmed != strings.ToLower(trimmed) {
		return "", fmt.Errorf("%w: key %q must use lowercase CLI form or exact LLAMA_ARG_* form", ErrInvalidINI, key)
	}
	if !canonicalKeyPattern.MatchString(trimmed) {
		return "", fmt.Errorf("%w: invalid key %q", ErrInvalidINI, key)
	}
	if alias, ok := keyAliases[trimmed]; ok {
		return alias, nil
	}
	return trimmed, nil
}

// parseINI parses models.ini content into its sections, applying the same key
// canonicalization and comment/grammar rules as the renderer. It is the
// counterpart used for round-trip checks and migration of existing files.
func parseINI(data []byte) (map[string]section, error) {
	sections := map[string]section{}
	current := ""
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			name, err := parseHeader(trimmed, sections)
			if err != nil {
				return nil, err
			}
			current = name
			continue
		}
		if current == "" {
			continue // version and future file-level keys
		}
		if err := parseValueLine(trimmed, sections[current].Values); err != nil {
			return nil, err
		}
	}
	return sections, nil
}

func parseHeader(trimmed string, sections map[string]section) (string, error) {
	if !strings.HasSuffix(trimmed, "]") {
		return "", fmt.Errorf("%w: malformed section header %q", ErrInvalidINI, trimmed)
	}
	name := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
	if err := validateSectionName(name); err != nil {
		return "", err
	}
	if _, exists := sections[name]; exists {
		return "", fmt.Errorf("%w: duplicate section %q", ErrInvalidINI, name)
	}
	sections[name] = section{Name: name, Values: map[string]string{}}
	return name, nil
}

func parseValueLine(trimmed string, values map[string]string) error {
	key, value, ok := strings.Cut(trimmed, "=")
	if !ok {
		return fmt.Errorf("%w: expected key = value, got %q", ErrInvalidINI, trimmed)
	}
	canonical, err := canonicalKey(strings.TrimSpace(key))
	if err != nil {
		return err
	}
	if comment := inlineCommentPattern.FindStringIndex(value); comment != nil {
		value = value[:comment[0]]
	}
	values[canonical] = strings.TrimSpace(value)
	return nil
}
