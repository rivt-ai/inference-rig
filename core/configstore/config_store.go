// Package configstore is the generic persistence layer for the neutral
// application config file. It reads, validates, and atomically rewrites
// config.yaml (via platform/filedoc) while preserving comments and formatting.
// Backend-specific profile stores build on this mechanism; the canonical YAML
// profile store lands in a later phase.
package configstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"inferencerig/config"
	"inferencerig/platform/filedoc"

	"gopkg.in/yaml.v3"
)

const DefaultLimitBytes int64 = 2 << 20

var (
	ErrEmpty     = errors.New("config.yaml content is empty")
	ErrTooLarge  = errors.New("config.yaml content exceeds size limit")
	ErrMalformed = errors.New("config.yaml content is malformed")
)

// SetStartupServices replaces the top-level startup_services sequence while
// preserving unrelated YAML nodes and comments.
func (s *FileStore) SetStartupServices(ctx context.Context, services []string) (WriteResult, error) {
	return s.mutateDocument(ctx, func(document *yaml.Node) bool {
		root := documentRoot(document)
		if root == nil {
			return false
		}
		current := mappingValue(root, "startup_services")
		if sequenceEquals(current, services) {
			return false
		}
		if current == nil {
			root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "startup_services"}, &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"})
			current = root.Content[len(root.Content)-1]
		}
		current.Kind, current.Tag, current.Value = yaml.SequenceNode, "!!seq", ""
		current.Content = make([]*yaml.Node, 0, len(services))
		for _, service := range services {
			current.Content = append(current.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: service})
		}
		return true
	})
}

// SetProfileAutostart adds or removes a profile from the neutral top-level
// autostart list while preserving unrelated YAML nodes and comments.
func (s *FileStore) SetProfileAutostart(ctx context.Context, name string, enabled bool) (WriteResult, error) {
	return s.mutateDocument(ctx, func(document *yaml.Node) bool {
		return setProfileAutostart(documentRoot(document), name, enabled)
	})
}

func setProfileAutostart(root *yaml.Node, name string, enabled bool) bool {
	if root == nil {
		return false
	}
	current := mappingValue(root, "autostart_profiles")
	if enabled {
		return addProfileAutostart(root, current, name)
	}
	return removeProfileAutostart(current, name)
}

func addProfileAutostart(root, current *yaml.Node, name string) bool {
	if current == nil {
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "autostart_profiles"},
			&yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"})
		current = root.Content[len(root.Content)-1]
	}
	for _, item := range current.Content {
		if item.Value == name {
			return false
		}
	}
	current.Content = append(current.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name})
	return true
}

func removeProfileAutostart(current *yaml.Node, name string) bool {
	if current == nil || current.Kind != yaml.SequenceNode {
		return false
	}
	kept := current.Content[:0]
	for _, item := range current.Content {
		if item.Value != name {
			kept = append(kept, item)
		}
	}
	changed := len(kept) != len(current.Content)
	current.Content = kept
	return changed
}

// mutateDocument applies mutate to the parsed config.yaml document and, when
// it reports a change, validates and atomically rewrites the file preserving
// comments and formatting. The file must already be valid: an ordinary edit
// has no business rewriting a config nobody has diagnosed.
func (s *FileStore) mutateDocument(ctx context.Context, mutate func(*yaml.Node) bool) (WriteResult, error) {
	return s.applyDocument(ctx, mutate, true)
}

// Repair applies mutate to a config.yaml that does not currently load, then
// validates the result before writing. It is how a diagnostic fixes the file
// that is actually broken — the one mutateDocument refuses to touch.
//
// The output is still validated, so a repair cannot leave the file worse than
// it found it. It cannot fix YAML syntax errors: the document must still parse
// into a node tree for a mutation to have anything to act on.
func (s *FileStore) Repair(ctx context.Context, mutate func(*yaml.Node) bool) (WriteResult, error) {
	return s.applyDocument(ctx, mutate, false)
}

func (s *FileStore) applyDocument(
	ctx context.Context, mutate func(*yaml.Node) bool, requireLoadable bool,
) (WriteResult, error) {
	if err := ctx.Err(); err != nil {
		return WriteResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.readForMutation(requireLoadable)
	if err != nil {
		return WriteResult{}, err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return WriteResult{}, fmt.Errorf("parse config YAML document: %w", err)
	}
	if !mutate(&document) {
		return WriteResult{}, nil
	}
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(&document); err != nil {
		return WriteResult{}, fmt.Errorf("encode config YAML document: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return WriteResult{}, err
	}
	if err := s.Validate(ctx, out.String()); err != nil {
		return WriteResult{}, err
	}
	return s.replaceLocked(out.String())
}

func sequenceEquals(node *yaml.Node, values []string) bool {
	if node == nil || node.Kind != yaml.SequenceNode || len(node.Content) != len(values) {
		return false
	}
	for i, value := range values {
		if node.Content[i].Value != value {
			return false
		}
	}
	return true
}

// documentRoot returns the top-level mapping node of a parsed YAML document.
func documentRoot(document *yaml.Node) *yaml.Node {
	if document == nil || len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	return document.Content[0]
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

type WriteResult = filedoc.WriteResult

type FileStore struct {
	path       string
	limitBytes int64
	mu         sync.Mutex
}

func NewFileStore(path string, limitBytes int64) *FileStore {
	if limitBytes == 0 {
		limitBytes = DefaultLimitBytes
	}
	return &FileStore{path: filepath.Clean(path), limitBytes: limitBytes}
}

func (s *FileStore) Read(_ context.Context) (config.Config, error) {
	_, _, parsed, err := s.readParsed()
	return parsed, err
}

func (s *FileStore) Validate(_ context.Context, content string) error {
	data := []byte(content)
	if len(bytes.TrimSpace(data)) == 0 {
		return ErrEmpty
	}
	if int64(len(data)) > s.limitBytes {
		return ErrTooLarge
	}
	if _, err := config.Parse(data); err != nil {
		return fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	return nil
}

// readForMutation returns the raw file, optionally insisting it currently
// loads. Repair skips that precondition; the output check in applyDocument is
// what actually guards the write.
func (s *FileStore) readForMutation(requireLoadable bool) ([]byte, error) {
	if requireLoadable {
		_, data, _, err := s.readParsed()
		return data, err
	}
	info, err := os.Stat(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("config.yaml not found: %w", err)
		}
		return nil, fmt.Errorf("stat config.yaml: %w", err)
	}
	if info.Size() > s.limitBytes {
		return nil, ErrTooLarge
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("read config.yaml: %w", err)
	}
	return data, nil
}

func (s *FileStore) readParsed() (os.FileInfo, []byte, config.Config, error) {
	info, err := os.Stat(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, config.Config{}, fmt.Errorf("config.yaml not found: %w", err)
		}
		return nil, nil, config.Config{}, fmt.Errorf("stat config.yaml: %w", err)
	}
	if info.Size() > s.limitBytes {
		return nil, nil, config.Config{}, ErrTooLarge
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, nil, config.Config{}, fmt.Errorf("read config.yaml: %w", err)
	}
	parsed, err := config.Parse(data)
	if err != nil {
		return nil, nil, config.Config{}, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	return info, data, parsed, nil
}

func (s *FileStore) replaceLocked(content string) (WriteResult, error) {
	_, err := os.Stat(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return WriteResult{}, fmt.Errorf("config.yaml not found: %w", err)
		}
		return WriteResult{}, fmt.Errorf("stat config.yaml: %w", err)
	}
	result, err := filedoc.WriteFile(s.path, content, filedoc.WriteOptions{Backup: true, Normalize: normalize})
	if err != nil {
		return WriteResult{}, fmt.Errorf("replace config.yaml: %w", err)
	}
	return result, nil
}

func normalize(content string) string {
	return string(bytes.TrimRight([]byte(content), "\n")) + "\n"
}
