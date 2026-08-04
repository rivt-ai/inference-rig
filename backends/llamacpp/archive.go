package llamacpp

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// entryMode is the permission mask applied to extracted files: an archive may
// ask for less, never for more, and never for setuid, setgid or sticky.
const entryMode = 0o755

// extractTarGz unpacks archive into destination with archive/tar rather than
// the tar(1) binary, so every entry is validated before anything is written: no
// absolute path, no traversal above destination, no symlink pointing out of it,
// and no entry type beyond a directory, a regular file or a link. A rejected
// entry fails the whole extraction — a partially trusted archive is not
// installable.
func extractTarGz(archive, destination string) error {
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	root, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("extract archive: %w", err)
	}
	defer func() { _ = gz.Close() }()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("extract archive: %w", err)
		}
		if err := extractEntry(reader, root, header); err != nil {
			return fmt.Errorf("extract archive: %w", err)
		}
	}
}

func extractEntry(reader io.Reader, root string, header *tar.Header) error {
	target, err := entryPath(root, header.Name)
	if err != nil {
		return err
	}
	switch header.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(target, 0o755)
	case tar.TypeReg:
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return writeEntry(reader, target, header)
	case tar.TypeSymlink, tar.TypeLink:
		resolved, err := entryPath(root, linkTarget(header))
		if err != nil {
			return fmt.Errorf("link %q: %w", header.Name, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if header.Typeflag == tar.TypeLink {
			source, err := entryPath(root, header.Linkname)
			if err != nil {
				return err
			}
			return os.Link(source, target)
		}
		// Link with a path recomputed from the validated destination rather
		// than with the raw header value, so the bytes written into the
		// symlink cannot differ from what was checked against the root.
		link, err := filepath.Rel(filepath.Dir(target), resolved)
		if err != nil {
			return fmt.Errorf("link %q: %w", header.Name, err)
		}
		return os.Symlink(link, target)
	default:
		return fmt.Errorf("entry %q has unsupported type %q", header.Name, string(header.Typeflag))
	}
}

// entryPath resolves name against root and rejects anything that would land
// outside it: an absolute path, a traversal, a link target climbing out, or a
// path that reaches out through a symlink an earlier entry planted.
func entryPath(root, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if !filepath.IsLocal(clean) {
		return "", fmt.Errorf("entry %q escapes the install directory", name)
	}
	joined := filepath.Join(root, clean)
	// IsLocal rejects traversal in the name itself, but an earlier entry may
	// have planted a symlinked directory that a later entry writes through.
	// Resolve both sides and require the entry to stay under the real root.
	realRoot, err := evalExistingPrefix(root)
	if err != nil {
		return "", err
	}
	realPath, err := evalExistingPrefix(joined)
	if err != nil {
		return "", err
	}
	if realPath != realRoot && !strings.HasPrefix(realPath, realRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("entry %q escapes the install directory", name)
	}
	return joined, nil
}

// evalExistingPrefix resolves symlinks in the deepest part of path that exists
// and re-attaches the components that do not. filepath.EvalSymlinks fails on a
// missing path, and an archive entry is missing until it is written.
func evalExistingPrefix(path string) (string, error) {
	missing := ""
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			return filepath.Join(resolved, missing), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		missing = filepath.Join(filepath.Base(path), missing)
		path = parent
	}
}

// linkTarget is the path a link entry resolves to, relative to the link's own
// directory when the target is relative.
func linkTarget(header *tar.Header) string {
	if strings.HasPrefix(header.Linkname, "/") || header.Typeflag == tar.TypeLink {
		return header.Linkname
	}
	return filepath.Join(filepath.Dir(filepath.FromSlash(header.Name)), filepath.FromSlash(header.Linkname))
}

func writeEntry(reader io.Reader, target string, header *tar.Header) error {
	mode := header.FileInfo().Mode()
	if mode&^os.ModePerm != 0 {
		return fmt.Errorf("entry %q has unexpected mode %v", header.Name, mode)
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm()&entryMode)
	if err != nil {
		return err
	}
	// CopyN bounds the write by the declared entry size, so a header that lies
	// about its length cannot fill the disk.
	written, err := io.CopyN(file, reader, header.Size)
	if closeErr := file.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if errors.Is(err, io.EOF) && written == header.Size {
		return nil
	}
	return err
}
