package llamacpp

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// writeArchive builds a .tar.gz of headers (each regular entry carries "x" as
// its body) so a crafted archive can be handed to the extractor.
func writeArchive(t *testing.T, headers ...*tar.Header) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "crafted.tar.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	writer := tar.NewWriter(gz)
	for _, header := range headers {
		if header.Typeflag == tar.TypeReg {
			header.Size = 1
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			if _, err := writer.Write([]byte("x")); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, closer := range []func() error{writer.Close, gz.Close, file.Close} {
		if err := closer(); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestExtractTarGzRejectsHostileEntries(t *testing.T) {
	cases := map[string]*tar.Header{
		"traversal":     {Name: "../escaped", Typeflag: tar.TypeReg, Mode: 0o644},
		"nested":        {Name: "build/../../escaped", Typeflag: tar.TypeReg, Mode: 0o644},
		"absolute":      {Name: "/etc/escaped", Typeflag: tar.TypeReg, Mode: 0o644},
		"symlink":       {Name: "link", Linkname: "../escaped", Typeflag: tar.TypeSymlink},
		"deep symlink":  {Name: "bin/link", Linkname: "../../../escaped", Typeflag: tar.TypeSymlink},
		"absolute link": {Name: "link", Linkname: "/etc/passwd", Typeflag: tar.TypeSymlink},
		"hard link":     {Name: "link", Linkname: "../escaped", Typeflag: tar.TypeLink},
		"device":        {Name: "dev", Typeflag: tar.TypeChar, Mode: 0o644},
		"fifo":          {Name: "pipe", Typeflag: tar.TypeFifo, Mode: 0o644},
		"setuid":        {Name: "suid", Typeflag: tar.TypeReg, Mode: 0o4755},
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			destination := filepath.Join(root, "install")
			if err := extractTarGz(writeArchive(t, header), destination); err == nil {
				t.Fatal("hostile entry accepted")
			}
			if _, err := os.Lstat(filepath.Join(root, "escaped")); !os.IsNotExist(err) {
				t.Fatalf("extraction wrote outside the target: %v", err)
			}
		})
	}
}

func TestExtractTarGzWritesRegularEntries(t *testing.T) {
	archive := writeArchive(t,
		&tar.Header{Name: "build", Typeflag: tar.TypeDir, Mode: 0o755},
		&tar.Header{Name: "build/llama-server", Typeflag: tar.TypeReg, Mode: 0o755},
		&tar.Header{Name: "build/libllama.so", Typeflag: tar.TypeReg, Mode: 0o644},
		&tar.Header{Name: "build/libllama.so.1", Linkname: "libllama.so", Typeflag: tar.TypeSymlink},
	)
	destination := filepath.Join(t.TempDir(), "install")
	if err := extractTarGz(archive, destination); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(destination, "build", "llama-server"))
	if err != nil || info.Mode().Perm() != 0o755 || info.Size() != 1 {
		t.Fatalf("extracted binary = %v, err = %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(destination, "build", "libllama.so.1")); err != nil {
		t.Fatalf("symlink inside the target rejected: %v", err)
	}
}
