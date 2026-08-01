package control

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"inferencerig/core/runtime"
)

func newTestJournal(t *testing.T, limit int) (*FileJournal, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state", "failures.jsonl")
	journal := NewFileJournal(path, limit)
	return journal, path
}

// The journal exists to answer "what went wrong" after the daemon that held the
// in-memory history is gone, so a failure has to survive as a readable record.
func TestFileJournalPersistsFailures(t *testing.T) {
	journal, path := newTestJournal(t, 0)

	journal.Record(context.Background(), AuditEvent{
		Action: "runtime.start", Success: false, ErrorKind: ErrorTimeout,
		Detail: "readiness timed out", Profile: "demo", Backend: "llamacpp",
		State: runtime.Failed, Recovery: runtime.RecoveryStalePIDFile,
	})

	entries, err := journal.Recent(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Action != "runtime.start" || entry.Profile != "demo" || entry.Detail != "readiness timed out" {
		t.Errorf("entry = %+v", entry)
	}
	// AuditEvent has no timestamp, so the journal must supply one or the record
	// cannot be placed in time.
	if entry.Time.IsZero() {
		t.Error("entry has no timestamp")
	}
	if entry.Recovery != string(runtime.RecoveryStalePIDFile) {
		t.Errorf("recovery = %q, want the classification carried through", entry.Recovery)
	}

	// The journal can name a failing profile, so it must not be world-readable.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %04o, want 0600", info.Mode().Perm())
	}
}

// Successes are the overwhelming majority and would push failures out of a
// bounded file.
func TestFileJournalIgnoresSuccesses(t *testing.T) {
	journal, path := newTestJournal(t, 0)

	journal.Record(context.Background(), AuditEvent{Action: "runtime.start", Success: true})

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("a successful event created the journal: %v", err)
	}
	entries, err := journal.Recent(0)
	if err != nil || len(entries) != 0 {
		t.Errorf("entries = %v, err = %v, want none", entries, err)
	}
}

// An append-only journal must not grow without bound.
func TestFileJournalCompactsPastItsLimit(t *testing.T) {
	journal, _ := newTestJournal(t, 3)

	for i := range 10 {
		journal.Record(context.Background(), AuditEvent{
			Action: "runtime.start", Success: false, Detail: string(rune('a' + i)),
		})
	}

	entries, err := journal.Recent(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want the limit of 3", len(entries))
	}
	// Newest last, and the oldest dropped.
	if entries[2].Detail != "j" || entries[0].Detail != "h" {
		t.Errorf("entries = %+v, want the newest three in order", entries)
	}
}

func TestFileJournalRecentHonoursLimit(t *testing.T) {
	journal, _ := newTestJournal(t, 0)
	for i := range 5 {
		journal.Record(context.Background(), AuditEvent{
			Action: "runtime.stop", Success: false, Detail: string(rune('a' + i)),
		})
	}

	entries, err := journal.Recent(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[1].Detail != "e" {
		t.Errorf("entries = %+v, want the newest two", entries)
	}
}

// Nothing has failed yet is not an error.
func TestFileJournalMissingFileIsEmpty(t *testing.T) {
	journal, _ := newTestJournal(t, 0)

	entries, err := journal.Recent(0)
	if err != nil || len(entries) != 0 {
		t.Errorf("entries = %v, err = %v, want empty and no error", entries, err)
	}
}

// A write interrupted mid-line must not make the whole history unreadable.
func TestFileJournalSkipsATruncatedLine(t *testing.T) {
	journal, path := newTestJournal(t, 0)
	journal.Record(context.Background(), AuditEvent{Action: "runtime.start", Success: false, Detail: "first"})
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"action":"runtime.st`); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()

	entries, err := journal.Recent(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Detail != "first" {
		t.Errorf("entries = %+v, want the intact record", entries)
	}
}

// A journal write must never take down the operation it is recording.
func TestFileJournalSurvivesAnUnwritablePath(t *testing.T) {
	journal := NewFileJournal(filepath.Join(t.TempDir(), "file", "failures.jsonl"), 0)
	if err := os.WriteFile(filepath.Dir(filepath.Dir(journal.path))+"/file", nil, 0o600); err != nil {
		t.Fatal(err)
	}

	journal.Record(context.Background(), AuditEvent{Action: "runtime.start", Success: false})
	// No panic and no propagated error is the whole assertion.
}

func TestFileJournalIsAnAuditSink(t *testing.T) {
	var sink AuditSink = NewFileJournal(filepath.Join(t.TempDir(), "failures.jsonl"), 0)
	sink.Record(context.Background(), AuditEvent{Action: "x", Success: false, Duration: time.Second})
}
