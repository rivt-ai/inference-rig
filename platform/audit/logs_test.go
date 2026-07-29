package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"inferencerig/config"
)

func writeLog(t *testing.T, name, content string) {
	t.Helper()
	path, err := GetLogPath(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTailLogReturnsWholeFileWhenSmall(t *testing.T) {
	t.Setenv(config.ProjectHomeEnv, t.TempDir())
	writeLog(t, config.ProjectName, "line1\nline2\n")

	text, err := TailLogLines(config.ProjectName, 10)
	if err != nil {
		t.Fatal(err)
	}
	if text != "line1\nline2\n" {
		t.Fatalf("text = %q", text)
	}
}

func TestTailLogLinesLimitsOutput(t *testing.T) {
	t.Setenv(config.ProjectHomeEnv, t.TempDir())
	writeLog(t, config.ProjectName, "aaaa\nbbbb\ncccc\n")

	// Window of 10 bytes lands mid-file; the partial first line must be dropped.
	text, err := TailLogLines(config.ProjectName, 2)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "aaaa") {
		t.Fatalf("expected partial leading line dropped, got %q", text)
	}
	if !strings.HasSuffix(text, "cccc\n") {
		t.Fatalf("expected tail to end with last line, got %q", text)
	}
}

func TestTailLogMissingFileIsEmpty(t *testing.T) {
	t.Setenv(config.ProjectHomeEnv, t.TempDir())

	text, err := TailLogLines(config.ProjectName, 10)
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if text != "" {
		t.Fatalf("text = %q, want empty", text)
	}
}

func TestTailLogLinesReturnsLastLines(t *testing.T) {
	t.Setenv(config.ProjectHomeEnv, t.TempDir())
	writeLog(t, config.ProjectName, "one\ntwo\nthree\n")

	text, err := TailLogLines(config.ProjectName, 2)
	if err != nil {
		t.Fatal(err)
	}
	if text != "two\nthree\n" {
		t.Fatalf("text = %q", text)
	}
}

func TestTailLogLinesCapsNewlineFreeInput(t *testing.T) {
	t.Setenv(config.ProjectHomeEnv, t.TempDir())
	writeLog(t, config.ProjectName, strings.Repeat("x", maxTailReadBytes+1))

	text, err := TailLogLines(config.ProjectName, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(text) != maxTailReadBytes {
		t.Fatalf("tail bytes = %d, want %d", len(text), maxTailReadBytes)
	}
}

func TestArchiveLogListsTailsAndDeletes(t *testing.T) {
	t.Setenv(config.ProjectHomeEnv, t.TempDir())
	writeLog(t, config.ProjectName, "old\nlog\n")
	now := time.Date(2026, 7, 3, 12, 30, 0, 123, time.UTC)
	id, err := ArchiveLog(config.ProjectName, now)
	if err != nil {
		t.Fatal(err)
	}
	archives, err := ListArchives()
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 1 || archives[0].ID != id || !archives[0].ArchivedAt.Equal(now) {
		t.Fatalf("archives = %#v", archives)
	}
	text, err := TailArchive(id, 1)
	if err != nil || text != "log\n" {
		t.Fatalf("TailArchive text=%q err=%v", text, err)
	}
	if err := DeleteArchive(id); err != nil {
		t.Fatal(err)
	}
	if err := DeleteArchive(id); err != nil {
		t.Fatalf("repeated delete: %v", err)
	}
	archives, err = ListArchives()
	if err != nil || len(archives) != 0 {
		t.Fatalf("archives=%#v err=%v", archives, err)
	}
}

func TestCleanupArchivesHonorsRetentionAndZero(t *testing.T) {
	t.Setenv(config.ProjectHomeEnv, t.TempDir())
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	for _, archivedAt := range []time.Time{now.Add(-8 * 24 * time.Hour), now.Add(-6 * 24 * time.Hour)} {
		writeLog(t, config.ProjectName, archivedAt.String())
		if _, err := ArchiveLog(config.ProjectName, archivedAt); err != nil {
			t.Fatal(err)
		}
	}
	if deleted, err := CleanupArchives(0, now); err != nil || deleted != 0 {
		t.Fatalf("disabled cleanup deleted=%d err=%v", deleted, err)
	}
	if deleted, err := CleanupArchives(7*24*time.Hour, now); err != nil || deleted != 1 {
		t.Fatalf("cleanup deleted=%d err=%v", deleted, err)
	}
}

func TestArchiveRejectsTraversal(t *testing.T) {
	t.Setenv(config.ProjectHomeEnv, t.TempDir())
	if _, err := TailArchive("../inferencerig.log", 10); err == nil {
		t.Fatal("TailArchive accepted traversal")
	}
}
