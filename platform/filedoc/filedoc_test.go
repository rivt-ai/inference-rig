package filedoc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileReplacesWithBackupNormalizeAndHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("old\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	result, err := WriteFile(path, "new\n\n", WriteOptions{
		Backup: true,
		Normalize: func(content string) string {
			return strings.TrimRight(content, "\n") + "\n"
		},
	})
	if err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if result.BackupPath == "" || result.SHA256 != SHA256Hex([]byte("new\n")) || result.SizeBytes != 4 {
		t.Fatalf("result = %#v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new\n" {
		t.Fatalf("data = %q", data)
	}
	backup, err := os.ReadFile(result.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != "old\n" {
		t.Fatalf("backup = %q", backup)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("perm = %v", info.Mode().Perm())
	}
}

func TestWriteFileBackupNamesDoNotCollide(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("old: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := WriteFile(path, "[first]\n", WriteOptions{Backup: true})
	if err != nil {
		t.Fatalf("first WriteFile returned error: %v", err)
	}
	second, err := WriteFile(path, "[second]\n", WriteOptions{Backup: true})
	if err != nil {
		t.Fatalf("second WriteFile returned error: %v", err)
	}
	if first.BackupPath == second.BackupPath {
		t.Fatalf("backup path collision: %q", first.BackupPath)
	}
}

func TestAtomicCreateWritesPerm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "created.txt")
	if err := AtomicCreate(path, []byte("created"), 0o600); err != nil {
		t.Fatalf("AtomicCreate returned error: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v", info.Mode().Perm())
	}
}
