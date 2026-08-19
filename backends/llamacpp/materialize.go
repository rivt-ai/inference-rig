package llamacpp

import (
	"fmt"
	"path/filepath"
	"strconv"

	"inferencerig/backends"
	"inferencerig/config"
	"inferencerig/core/profiles"
)

// modelKey is the models.ini key naming the model file a section serves.
const modelKey = "model"

// generatedFileMode is the permission of the generated models.ini.
const generatedFileMode = 0o600

// Materialize renders the effective profile into the generated models.ini as a
// neutral Materialization. The file carries the backend-wide defaults as [*]
// and the profile as its named section; its content is deterministic. The
// returned GeneratedFile records the path, bytes and mode; Materialize does not
// touch disk — Generate performs the atomic replacement.
func (b *Backend) Materialize(p profiles.Profile) (backends.Materialization, error) {
	return b.MaterializeProfiles([]profiles.Profile{p})
}

// MaterializeProfiles renders every effective profile into one generated
// models.ini. The neutral control manager discovers this optional batch facet
// without importing the concrete backend.
func (b *Backend) MaterializeProfiles(ps []profiles.Profile) (backends.Materialization, error) {
	path, err := b.generatedININPath()
	if err != nil {
		return backends.Materialization{}, err
	}
	content, err := b.render(ps)
	if err != nil {
		return backends.Materialization{}, err
	}
	return backends.Materialization{
		Files: []backends.GeneratedFile{
			{Path: path, Content: []byte(content), Mode: generatedFileMode},
			// The baseline copy, so the next materialization can tell a manual
			// edit from a profile change. It rides in the same Materialization
			// rather than being written separately: the neutral writer replaces
			// each file atomically, and a copy written by any other path could
			// disagree with the file it is supposed to be a record of.
			{Path: path + baselineSuffix, Content: []byte(content), Mode: generatedFileMode},
		},
		Summary: fmt.Sprintf("rendered %d models.ini sections", len(ps)),
	}, nil
}

// render turns the backend defaults and profiles into deterministic models.ini
// text, validating every section as it goes. The model storage directory is
// resolved once, because each section names its model by absolute path.
func (b *Backend) render(ps []profiles.Profile) (string, error) {
	storage, err := b.modelStorageDir()
	if err != nil {
		return "", err
	}
	sections := make([]section, 0, len(ps))
	for _, p := range ps {
		s, err := profileSection(p, storage)
		if err != nil {
			return "", err
		}
		sections = append(sections, s)
	}
	return renderModelsINI(b.opts.Defaults, sections)
}

// profileSection maps one canonical profile onto its models.ini stanza: the
// model source becomes `model`, and each engine_arg becomes a canonical key.
func profileSection(p profiles.Profile, storageDir string) (section, error) {
	if err := validateSectionName(p.Name); err != nil {
		return section{}, err
	}
	values, err := engineArgValues(p.EngineArgs)
	if err != nil {
		return section{}, err
	}
	if p.Model.Source != "" {
		values[modelKey] = modelPath(p.Model, storageDir)
	}
	return section{Name: p.Name, Values: values}, nil
}

// modelPath is the absolute path of the profile's model file. A repo id, a
// download URL or a bare filename all name an artifact the downloader lands in
// model storage under its base name (see Plan), so the same base name is what
// the section points at; an absolute source is already a path and is kept. The
// path is absolute so the preset resolves without --models-dir, which the
// router is given only when it may serve models no profile declares.
func modelPath(m profiles.ModelSpec, storageDir string) string {
	if expanded := config.ExpandHome(m.Source); filepath.IsAbs(expanded) {
		return expanded
	}
	name, _ := resolveArtifact(m.Source, m.Reference)
	return filepath.Join(storageDir, name)
}

// engineArgValues converts a profile's free-form engine_args into canonical
// models.ini string values, rejecting keys or scalars that cannot render.
func engineArgValues(args map[string]any) (map[string]string, error) {
	values := make(map[string]string, len(args))
	for key, raw := range args {
		canonical, err := canonicalKey(key)
		if err != nil {
			return nil, err
		}
		if _, exists := values[canonical]; exists {
			return nil, fmt.Errorf("%w: duplicate engine_arg %q", ErrInvalidINI, canonical)
		}
		text, err := scalarString(raw)
		if err != nil {
			return nil, fmt.Errorf("engine_arg %q: %w", key, err)
		}
		values[canonical] = text
	}
	return values, nil
}

// scalarString renders a YAML scalar engine_arg value to its models.ini text.
func scalarString(raw any) (string, error) {
	switch v := raw.(type) {
	case string:
		return v, nil
	case bool:
		return strconv.FormatBool(v), nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), nil
	case nil:
		return "", fmt.Errorf("%w: value is null", ErrInvalidINI)
	default:
		return "", fmt.Errorf("%w: unsupported value type %T", ErrInvalidINI, raw)
	}
}
