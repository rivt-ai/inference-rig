package profiles

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"inferencerig/platform/filedoc"

	"gopkg.in/yaml.v3"
)

var (
	// ErrInvalid marks a profile that fails schema or common-field validation.
	ErrInvalid = errors.New("profile is invalid")
	// ErrTooLarge marks a profile.yaml exceeding the size limit.
	ErrTooLarge = errors.New("profile exceeds size limit")
	// ErrExists marks a create for a profile that already exists.
	ErrExists = errors.New("profile already exists")
)

const profileFileName = "profile.yaml"

// FileStore reads/writes canonical profiles under a root dir, one directory per
// profile (`<root>/<name>/profile.yaml`). Engine-agnostic: backend-specific
// validation is delegated to the injected BackendLookup.
type FileStore struct {
	root       string
	limitBytes int64
	lookup     BackendLookup
	mu         sync.Mutex
}

// NewFileStore builds a store rooted at root. limitBytes of 0 uses
// DefaultLimitBytes. lookup resolves each profile's backend to its validator.
func NewFileStore(root string, limitBytes int64, lookup BackendLookup) *FileStore {
	if limitBytes == 0 {
		limitBytes = DefaultLimitBytes
	}
	return &FileStore{root: filepath.Clean(root), limitBytes: limitBytes, lookup: lookup}
}

// Root returns the profiles root directory.

// List returns every profile under the root, sorted by name.
func (s *FileStore) List(ctx context.Context) ([]ProfileSummary, error) {
	docs, err := s.ListDocuments(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ProfileSummary, 0, len(docs))
	for _, doc := range docs {
		out = append(out, summaryOf(doc))
	}
	return out, nil
}

// ListDocuments returns every profile under the root as a full document, sorted
// by name. Building a summary already requires reading and validating the whole
// profile, so callers that want the documents should take them from here rather
// than following List with a Get per name.
func (s *FileStore) ListDocuments(ctx context.Context) ([]ProfileDocument, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read profiles dir: %w", err)
	}
	out := make([]ProfileDocument, 0, len(entries))
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !entry.IsDir() {
			continue
		}
		doc, err := s.Get(ctx, entry.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, doc)
	}
	// os.ReadDir returns entries sorted by filename and the loop only filters,
	// so out is already ordered by profile name.
	return out, nil
}

func summaryOf(doc ProfileDocument) ProfileSummary {
	return ProfileSummary{
		Name:            doc.Name,
		Dir:             doc.Dir,
		ProfileYAMLPath: doc.ProfileYAMLPath,
		Backend:         doc.Effective.Backend,
		Model:           doc.Effective.Model,
		Host:            doc.Effective.Listen.Host,
		Port:            doc.Effective.Listen.Port,
	}
}

// Get reads and validates the named profile.
func (s *FileStore) Get(ctx context.Context, name string) (ProfileDocument, error) {
	if err := ctx.Err(); err != nil {
		return ProfileDocument{}, err
	}
	dir, err := s.profileDir(name)
	if err != nil {
		return ProfileDocument{}, err
	}
	if err := rejectSymlink(dir); err != nil {
		return ProfileDocument{}, err
	}
	path := filepath.Join(dir, profileFileName)
	if err := rejectSymlink(path); err != nil {
		return ProfileDocument{}, err
	}
	data, err := s.readLimited(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ProfileDocument{}, fmt.Errorf("profile %q not found: %w", name, err)
		}
		return ProfileDocument{}, err
	}
	return s.buildDocument(name, dir, string(data))
}

// Validate parses and validates a candidate profile without persisting it.
func (s *FileStore) Validate(ctx context.Context, req CreateRequest) (ProfileDocument, error) {
	if err := ctx.Err(); err != nil {
		return ProfileDocument{}, err
	}
	dir, err := s.profileDir(req.Name)
	if err != nil {
		return ProfileDocument{}, err
	}
	return s.buildDocument(req.Name, dir, normalize(req.ProfileYAML))
}

func (s *FileStore) buildDocument(name, dir, profileYAML string) (ProfileDocument, error) {
	parsed, effective, err := s.parseEffective(name, profileYAML)
	if err != nil {
		return ProfileDocument{}, err
	}
	return ProfileDocument{
		Name:            name,
		Dir:             dir,
		ProfileYAMLPath: filepath.Join(dir, profileFileName),
		ProfileYAML:     profileYAML,
		Parsed:          parsed,
		Effective:       effective,
	}, nil
}

// Create validates and persists a new profile, failing if it already exists.
func (s *FileStore) Create(ctx context.Context, req CreateRequest) (ProfileDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.Validate(ctx, req)
	if err != nil {
		return ProfileDocument{}, err
	}
	if _, err := os.Lstat(doc.Dir); err == nil {
		return ProfileDocument{}, fmt.Errorf("%w: %s", ErrExists, req.Name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return ProfileDocument{}, err
	}
	if err := os.MkdirAll(doc.Dir, 0o700); err != nil {
		return ProfileDocument{}, fmt.Errorf("create profile dir: %w", err)
	}
	if err := os.Chmod(doc.Dir, 0o700); err != nil {
		return ProfileDocument{}, fmt.Errorf("chmod profile dir: %w", err)
	}
	if _, err := writeFile(doc.ProfileYAMLPath, doc.ProfileYAML, 0o600, false); err != nil {
		return ProfileDocument{}, err
	}
	return s.Get(ctx, req.Name)
}

// Replace re-validates content and atomically rewrites an existing profile.
func (s *FileStore) Replace(ctx context.Context, name, content string) (WriteResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.Get(ctx, name)
	if err != nil {
		return WriteResult{}, err
	}
	if _, _, err := s.parseEffective(name, normalize(content)); err != nil {
		return WriteResult{}, err
	}
	return writeFile(current.ProfileYAMLPath, content, 0o600, true)
}

// Delete removes a profile's directory and fsyncs the root.
func (s *FileStore) Delete(ctx context.Context, name string) (DeleteResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.Get(ctx, name)
	if err != nil {
		return DeleteResult{}, err
	}
	if err := os.RemoveAll(current.Dir); err != nil {
		return DeleteResult{}, fmt.Errorf("delete profile dir: %w", err)
	}
	return DeleteResult{Name: name, Dir: current.Dir}, filedoc.SyncDir(s.root)
}

func (s *FileStore) profileDir(name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	cleanDir := filepath.Clean(filepath.Join(s.root, name))
	if cleanDir != filepath.Join(s.root, name) {
		return "", fmt.Errorf("%w: profile path escapes root", ErrInvalid)
	}
	return cleanDir, nil
}

func (s *FileStore) readLimited(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	// Bound the read itself rather than trusting a prior Stat, so a file that
	// grows between the two cannot be read past the limit.
	data, err := io.ReadAll(io.LimitReader(file, s.limitBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > s.limitBytes {
		return nil, ErrTooLarge
	}
	return data, nil
}

// parseEffective decodes the profile, runs shared common-field validation, then
// delegates engine_args validation to the backend named by the profile.
func (s *FileStore) parseEffective(name, content string) (Profile, Profile, error) {
	if int64(len(content)) > s.limitBytes {
		return Profile{}, Profile{}, ErrTooLarge
	}
	if strings.TrimSpace(content) == "" {
		return Profile{}, Profile{}, fmt.Errorf("%w: profile.yaml is empty", ErrInvalid)
	}
	var parsed Profile
	dec := yaml.NewDecoder(strings.NewReader(content))
	dec.KnownFields(true)
	if err := dec.Decode(&parsed); err != nil {
		return Profile{}, Profile{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	effective, err := normalizeCommon(name, parsed)
	if err != nil {
		return Profile{}, Profile{}, err
	}
	effective, err = s.validateBackend(effective)
	if err != nil {
		return Profile{}, Profile{}, err
	}
	return parsed, effective, nil
}

// validateBackend resolves and invokes the backend validator for the profile.
func (s *FileStore) validateBackend(effective Profile) (Profile, error) {
	if s.lookup == nil {
		return Profile{}, fmt.Errorf("%w: no backend lookup configured", ErrInvalid)
	}
	validator, err := s.lookup(effective.Backend)
	if err != nil {
		return Profile{}, fmt.Errorf("%w: backend %q: %v", ErrInvalid, effective.Backend, err)
	}
	validated, err := validator.ValidateProfile(effective)
	if err != nil {
		return Profile{}, fmt.Errorf("%w: backend %q: %v", ErrInvalid, effective.Backend, err)
	}
	return validated, nil
}

func rejectSymlink(path string) error {
	err := filedoc.RejectSymlink(path)
	if errors.Is(err, filedoc.ErrSymlink) {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return err
}

func writeFile(path, content string, perm os.FileMode, backup bool) (WriteResult, error) {
	if backup {
		if err := rejectSymlink(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return WriteResult{}, err
		}
	}
	return filedoc.WriteFile(path, content, filedoc.WriteOptions{Perm: perm, Backup: backup, Normalize: normalize})
}

func normalize(content string) string {
	return string(bytes.TrimRight([]byte(content), "\n")) + "\n"
}
