package mlx

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
	"inferencerig/core/migrate"
	"inferencerig/core/profiles"
	"inferencerig/platform/filedoc"
)

const legacyProfileLimit = 2 << 20

type legacyProfile struct {
	Name              string            `yaml:"name"`
	Executable        string            `yaml:"executable"`
	Model             string            `yaml:"model"`
	Host              string            `yaml:"host"`
	Port              int               `yaml:"port"`
	Args              map[string]any    `yaml:"mlx_args"`
	Env               map[string]string `yaml:"env"`
	ReadinessPath     string            `yaml:"readiness_path"`
	ReadinessTimeout  any               `yaml:"readiness_timeout"`
	ReadinessInterval any               `yaml:"readiness_interval"`
}

// YAMLImporter previews legacy directory profiles as canonical profiles.
type YAMLImporter struct {
	root, host string
	portStart  int
}

// NewYAMLImporter creates a read-only directory-profile importer.
func NewYAMLImporter(root, host string, portStart int) *YAMLImporter {
	return &YAMLImporter{root: filepath.Clean(root), host: host, portStart: portStart}
}

// Preview scans base.yaml files without mutating the source tree.
func (i *YAMLImporter) Preview(ctx context.Context) ([]migrate.Candidate, error) {
	entries, err := os.ReadDir(i.root)
	if err != nil {
		return nil, fmt.Errorf("read legacy profiles: %w", err)
	}
	sort.Slice(entries, func(a, b int) bool { return entries[a].Name() < entries[b].Name() })
	out := make([]migrate.Candidate, 0, len(entries))
	for index, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		candidate, err := i.readCandidate(entry.Name(), index)
		if err != nil {
			return nil, err
		}
		out = append(out, candidate)
	}
	return out, nil
}

func (i *YAMLImporter) readCandidate(name string, index int) (migrate.Candidate, error) {
	path := filepath.Join(i.root, name, "base.yaml")
	if _, err := filedoc.StatRegular(path, legacyProfileLimit); err != nil {
		return migrate.Candidate{}, fmt.Errorf("inspect legacy profile %q: %w", name, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return migrate.Candidate{}, err
	}
	var source legacyProfile
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&source); err != nil {
		return migrate.Candidate{}, fmt.Errorf("parse legacy profile %q: %w", name, err)
	}
	if source.Name != "" && source.Name != name {
		return migrate.Candidate{}, fmt.Errorf("legacy profile name %q does not match directory %q", source.Name, name)
	}
	if source.Model == "" {
		return migrate.Candidate{}, fmt.Errorf("legacy profile %q has no model", name)
	}
	host := source.Host
	if host == "" {
		host = i.host
	}
	port := source.Port
	if port == 0 {
		port = i.portStart + index
	}
	canonical, err := yaml.Marshal(profiles.Profile{
		Version: 1, Name: name, Backend: Name,
		Model:      profiles.ModelSpec{Source: source.Model},
		Listen:     profiles.ListenSpec{Host: host, Port: port},
		EngineArgs: source.Args,
	})
	if err != nil {
		return migrate.Candidate{}, err
	}
	return migrate.Candidate{
		Name: name, SourcePath: path, ProfileYAML: string(canonical),
		Warnings: legacyWarnings(source),
	}, nil
}

func legacyWarnings(source legacyProfile) []string {
	var warnings []string
	if source.Executable != "" {
		warnings = append(warnings, "legacy executable is backend installation state and was not imported")
	}
	if len(source.Env) > 0 {
		warnings = append(warnings, "legacy environment entries require manual review")
	}
	if source.ReadinessPath != "" || source.ReadinessTimeout != nil || source.ReadinessInterval != nil {
		warnings = append(warnings, "legacy readiness overrides require manual review")
	}
	return warnings
}
