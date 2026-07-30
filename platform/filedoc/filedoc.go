package filedoc

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrSymlink = errors.New("symlink not allowed")

type WriteOptions struct {
	Perm      os.FileMode
	Backup    bool
	Normalize func(string) string
}

type WriteResult struct {
	Path       string `json:"path"`
	BackupPath string `json:"backup_path,omitempty"`
	SizeBytes  int64  `json:"size_bytes"`
	SHA256     string `json:"sha256"`
}

func WriteFile(path string, content string, opts WriteOptions) (WriteResult, error) {
	if opts.Normalize != nil {
		content = opts.Normalize(content)
	}
	return writeFile(path, []byte(content), opts)
}

func AtomicCreate(path string, data []byte, perm os.FileMode) error {
	_, err := writeFile(path, data, WriteOptions{Perm: perm})
	return err
}

func SyncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Sync()
}

func RejectSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s", ErrSymlink, path)
	}
	return nil
}

func RejectSymlinkAncestors(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	clean := filepath.Clean(abs)
	volume := filepath.VolumeName(clean)
	rest := clean[len(volume):]
	current := volume + string(os.PathSeparator)
	for _, part := range strings.Split(rest, string(os.PathSeparator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		if err := RejectSymlink(current); err != nil {
			return err
		}
	}
	return nil
}

func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeFile(path string, data []byte, opts WriteOptions) (WriteResult, error) {
	perm := opts.Perm
	if perm == 0 {
		perm = 0o600
	}
	if opts.Backup {
		backupPath, backupPerm, err := writeBackup(path)
		if err != nil {
			return WriteResult{}, err
		}
		perm = backupPerm
		return writeReplacement(path, data, perm, backupPath)
	}
	return writeReplacement(path, data, perm, "")
}

func writeBackup(path string) (string, os.FileMode, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, err
	}
	perm := info.Mode().Perm()
	current, err := os.ReadFile(path)
	if err != nil {
		return "", 0, fmt.Errorf("read backup source: %w", err)
	}
	backupPath := fmt.Sprintf("%s.backup-%s", path, timestampSuffix())
	if err := os.WriteFile(backupPath, current, perm); err != nil {
		return "", 0, fmt.Errorf("write backup: %w", err)
	}
	if err := syncFile(backupPath); err != nil {
		return "", 0, fmt.Errorf("sync backup: %w", err)
	}
	return backupPath, perm, nil
}

func writeReplacement(path string, data []byte, perm os.FileMode, backupPath string) (WriteResult, error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return WriteResult{}, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return WriteResult{}, fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return WriteResult{}, fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return WriteResult{}, fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return WriteResult{}, fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return WriteResult{}, fmt.Errorf("replace file: %w", err)
	}
	if err := SyncDir(dir); err != nil {
		return WriteResult{}, fmt.Errorf("sync directory: %w", err)
	}
	return WriteResult{Path: path, BackupPath: backupPath, SizeBytes: int64(len(data)), SHA256: SHA256Hex(data)}, nil
}

func syncFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Sync()
}

func timestampSuffix() string {
	var suffix [4]byte
	now := time.Now().UTC().Format("20060102-150405.000000000")
	if _, err := rand.Read(suffix[:]); err != nil {
		return now
	}
	return now + "-" + hex.EncodeToString(suffix[:])
}
