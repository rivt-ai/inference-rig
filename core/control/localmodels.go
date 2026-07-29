package control

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
)

// ProfilesUsingModel names the profiles whose model source points at path.
// Without it a client cannot tell a configured model from a stray download, and
// deleting one is a guess.
func (m *Manager) ProfilesUsingModel(ctx context.Context, path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	docs, err := m.ListProfileDocuments(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, doc := range docs {
		if referencesModel(doc.Effective.Model.Source, path) {
			names = append(names, doc.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// referencesModel reports whether a profile's model source designates path.
// A multi-file backend records the snapshot directory, so a profile pointing at
// the directory containing a file counts as referencing it.
func referencesModel(source, path string) bool {
	if source == "" {
		return false
	}
	source, path = filepath.Clean(source), filepath.Clean(path)
	if source == path {
		return true
	}
	return strings.HasPrefix(path, source+string(filepath.Separator))
}

// DeleteLocalModelCascade deletes a local model and, when cascade is set, the
// profiles that referenced it. Deleting a model without its profiles leaves
// them pointing at nothing, which fails only later at start time.
func (m *Manager) DeleteLocalModelCascade(
	ctx context.Context,
	backendName, path string,
	cascade bool,
) ([]string, error) {
	using, err := m.ProfilesUsingModel(ctx, path)
	if err != nil {
		return nil, err
	}
	if len(using) > 0 && !cascade {
		return using, Errorf(ErrorConflict,
			"local model is used by %s; pass cascade to remove them too", strings.Join(using, ", "))
	}
	if err := m.DeleteLocalModel(ctx, backendName, path); err != nil {
		return nil, err
	}
	removed := make([]string, 0, len(using))
	for _, name := range using {
		if _, err := m.DeleteProfile(ctx, name); err != nil {
			return removed, err
		}
		removed = append(removed, name)
	}
	return removed, nil
}
