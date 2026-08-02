package llamacpp

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"

	"gopkg.in/yaml.v3"

	"inferencerig/core/profiles"
)

// baselineSuffix names the copy of models.ini kept beside it, holding exactly
// what InferenceRig last wrote. It is the third side of the merge: without it a
// difference between the file and the profiles is ambiguous — someone edited the
// file, or the profiles moved on — and the two call for opposite actions.
const baselineSuffix = ".base"

// ImportGenerated carries manual models.ini edits back into canonical YAML.
//
// It is a three-way merge per profile section, between the baseline (what was
// last generated), the file as it now stands, and what the current profiles
// would render. Per key:
//
//   - file matches baseline: nobody edited it; the profile is already right.
//   - only the file moved: adopt the edit into the profile's YAML.
//   - both moved: a conflict. The profile wins and the key is reported.
//
// Only a profile's own section is importable. The [*] cascade holds backend-wide
// defaults that belong to no profile, and a section naming no profile has
// nothing to be imported into; edits to either are discarded by the next render,
// which is what the generated header says.
func (b *Backend) ImportGenerated(docs []profiles.ProfileDocument) (map[string]string, []string, error) {
	path, err := b.generatedININPath()
	if err != nil {
		return nil, nil, err
	}
	current, err := readOptional(path)
	if err != nil {
		return nil, nil, err
	}
	baseline, err := readOptional(path + baselineSuffix)
	if err != nil {
		return nil, nil, err
	}
	// No file, or no record of what we wrote, means no edit can be attributed to
	// a user; and an untouched file has nothing to attribute. Each is the normal
	// case, not a failure.
	if current == nil || baseline == nil || bytes.Equal(current, baseline) {
		return nil, nil, nil
	}
	edited, err := parseINI(current)
	if err != nil {
		return nil, nil, fmt.Errorf("read edited %s: %w", path, err)
	}
	base, err := parseINI(baseline)
	if err != nil {
		return nil, nil, fmt.Errorf("read baseline %s%s: %w", path, baselineSuffix, err)
	}
	return importSections(docs, edited, base)
}

func importSections(docs []profiles.ProfileDocument, edited, base map[string]section) (map[string]string, []string, error) {
	adopt := map[string]string{}
	var conflicts []string
	for _, doc := range docs {
		section, ok := edited[doc.Name]
		if doc.Effective.Backend != Name || !ok {
			continue // not ours, or a section deleted by hand and restored next render
		}
		want, err := profileSection(doc.Effective)
		if err != nil {
			return nil, nil, err
		}
		// The file already says what the profile says. This is both the ordinary
		// no-op and the crash-recovery case: if the baseline copy is stale
		// because a write was interrupted, the section still merges to nothing
		// rather than to a spurious conflict.
		if maps.Equal(section.Values, want.Values) {
			continue
		}
		merged, adopted, clashes := mergeSection(base[doc.Name].Values, section.Values, want.Values, doc.Parsed)
		for _, key := range clashes {
			conflicts = append(conflicts, doc.Name+"."+key)
		}
		if !adopted {
			continue
		}
		profileYAML, err := profiles.MergeYAML(doc.ProfileYAML, merged)
		if err != nil {
			return nil, nil, fmt.Errorf("profile %q: %w", doc.Name, err)
		}
		adopt[doc.Name] = profileYAML
	}
	return adopt, conflicts, nil
}

// mergeSection is the three-way merge for one section's keys, returning the
// profile with adopted edits applied, whether any were, and the keys that moved
// on both sides. Keys are walked in sorted order so the result and the conflict
// list are deterministic for identical input — a merge whose outcome depended on
// map iteration would be unreviewable and untestable.
//
// It merges into the *parsed* profile, not the effective one: only what the user
// wrote is written back, so defaults a backend filled in do not become explicit
// settings the user now owns.
func mergeSection(base, edited, want map[string]string, parsed profiles.Profile) (profiles.Profile, bool, []string) {
	merged := parsed
	merged.EngineArgs = maps.Clone(parsed.EngineArgs)
	union := maps.Clone(base)
	maps.Copy(union, edited)
	adopted := false
	var conflicts []string
	for _, key := range slices.Sorted(maps.Keys(union)) {
		value, inEdited := edited[key]
		baseValue, inBase := base[key]
		if inEdited == inBase && value == baseValue {
			continue // untouched by hand
		}
		if wantValue, inWant := want[key]; inWant != inBase || wantValue != baseValue {
			conflicts = append(conflicts, key)
			continue
		}
		adopt(&merged, key, value, inEdited)
		adopted = true
	}
	return merged, adopted, conflicts
}

// adopt writes one imported key onto the profile. `model` is the one key the
// renderer takes from a common field rather than engine_args, so it is the one
// key that must go back there.
func adopt(p *profiles.Profile, key, value string, present bool) {
	switch {
	case key == modelKey:
		p.Model.Source = value
	case !present:
		delete(p.EngineArgs, key)
	default:
		if p.EngineArgs == nil {
			p.EngineArgs = map[string]any{}
		}
		p.EngineArgs[key] = scalarValue(value)
	}
}

// scalarValue reads raw models.ini text as the YAML scalar it denotes, so an
// imported count lands in the profile as a number and a path as a string rather
// than every engine_arg turning into quoted text.
func scalarValue(text string) any {
	var probe any
	if err := yaml.Unmarshal([]byte(text), &probe); err == nil {
		switch probe.(type) {
		case bool, int, float64:
			return probe
		}
	}
	return text
}

func readOptional(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return data, err
}
