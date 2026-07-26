package llamacpp

import (
	"context"
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
	"inferencerig/core/migrate"
	"inferencerig/core/profiles"
)

// INIImporter previews legacy models.ini sections as canonical profiles.
type INIImporter struct {
	path, host string
	portStart  int
}

// NewINIImporter creates a read-only models.ini importer.
func NewINIImporter(path, host string, portStart int) *INIImporter {
	return &INIImporter{path: path, host: host, portStart: portStart}
}

// Preview parses the source once and returns deterministic canonical YAML.
func (i *INIImporter) Preview(ctx context.Context) ([]migrate.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Lstat(i.path)
	if err != nil {
		return nil, fmt.Errorf("inspect models.ini: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("models.ini source must be a regular non-symlink file")
	}
	data, err := os.ReadFile(i.path)
	if err != nil {
		return nil, fmt.Errorf("read models.ini: %w", err)
	}
	sections, err := parseINI(data)
	if err != nil {
		return nil, err
	}
	defaults := map[string]string{}
	if global, ok := sections[globalSection]; ok {
		defaults = global.Values
		delete(sections, globalSection)
	}
	names := make([]string, 0, len(sections))
	for name := range sections {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]migrate.Candidate, 0, len(names))
	for index, name := range names {
		candidate, err := i.candidate(name, sections[name], defaults, index)
		if err != nil {
			return nil, err
		}
		out = append(out, candidate)
	}
	return out, nil
}

func (i *INIImporter) candidate(name string, source section, defaults map[string]string, index int) (migrate.Candidate, error) {
	values := make(map[string]any, len(defaults)+len(source.Values))
	for key, value := range defaults {
		values[key] = value
	}
	for key, value := range source.Values {
		values[key] = value
	}
	model, ok := values[modelKey].(string)
	if !ok || model == "" {
		return migrate.Candidate{}, fmt.Errorf("models.ini section %q has no model", name)
	}
	delete(values, modelKey)
	data, err := yaml.Marshal(profiles.Profile{
		Version: 1, Name: name, Backend: Name,
		Model:      profiles.ModelSpec{Source: model},
		Listen:     profiles.ListenSpec{Host: i.host, Port: i.portStart + index},
		EngineArgs: values,
	})
	if err != nil {
		return migrate.Candidate{}, err
	}
	return migrate.Candidate{Name: name, SourcePath: i.path, ProfileYAML: string(data)}, nil
}
