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
// outside it: an absolute path, a traversal, or a link target climbing out.
func entryPath(root, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if !filepath.IsLocal(clean) {
		return "", fmt.Errorf("entry %q escapes the install directory", name)
	}
	joined := filepath.Join(root, clean)
	// IsLocal already rejects traversal, but assert containment on the joined
	// path too: it is the property that actually matters, and it is the form
	// static analysis can follow.
	if !strings.HasPrefix(joined, filepath.Clean(root)+string(os.PathSeparator)) {
		return "", fmt.Errorf("entry %q escapes the install directory", name)
	}
	return joined, nil
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
