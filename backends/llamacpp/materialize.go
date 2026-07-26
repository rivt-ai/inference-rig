package llamacpp

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"inferencerig/backends"
	"inferencerig/config"
	"inferencerig/core/profiles"
	"inferencerig/platform/filedoc"
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
	path, err := b.generatedININPath()
	if err != nil {
		return backends.Materialization{}, err
	}
	content, err := b.render([]profiles.Profile{p})
	if err != nil {
		return backends.Materialization{}, err
	}
	return backends.Materialization{
		Files: []backends.GeneratedFile{{
			Path:    path,
			Content: []byte(content),
			Mode:    generatedFileMode,
		}},
		Summary: fmt.Sprintf("rendered models.ini section [%s]", p.Name),
	}, nil
}

// Generate renders every profile plus the backend-wide defaults into the
// generated models.ini and replaces it atomically. Rendering fully precedes the
// write, so an invalid profile set returns an error and never replaces the last
// valid file. The router's models source is then refreshed by rereading it.
func (b *Backend) Generate(ps []profiles.Profile) (filedoc.WriteResult, error) {
	path, err := b.generatedININPath()
	if err != nil {
		return filedoc.WriteResult{}, err
	}
	content, err := b.render(ps)
	if err != nil {
		return filedoc.WriteResult{}, err
	}
	if mkErr := os.MkdirAll(filepath.Dir(path), 0o700); mkErr != nil {
		return filedoc.WriteResult{}, mkErr
	}
	return filedoc.WriteFile(path, content, filedoc.WriteOptions{Perm: generatedFileMode})
}

// render turns the backend defaults and profiles into deterministic models.ini
// text, validating every section as it goes.
func (b *Backend) render(ps []profiles.Profile) (string, error) {
	sections := make([]section, 0, len(ps))
	for _, p := range ps {
		s, err := profileSection(p)
		if err != nil {
			return "", err
		}
		sections = append(sections, s)
	}
	return renderModelsINI(b.opts.Defaults, sections)
}

// profileSection maps one canonical profile onto its models.ini stanza: the
// model source becomes `model`, and each engine_arg becomes a canonical key.
func profileSection(p profiles.Profile) (section, error) {
	if err := validateSectionName(p.Name); err != nil {
		return section{}, err
	}
	values, err := engineArgValues(p.EngineArgs)
	if err != nil {
		return section{}, err
	}
	if p.Model.Source != "" {
		values[modelKey] = config.ExpandHome(p.Model.Source)
	}
	return section{Name: p.Name, Values: values}, nil
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
