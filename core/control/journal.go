package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DefaultJournalLimit caps how many failures are retained.
const DefaultJournalLimit = 200

// JournalEntry is one recorded failure. AuditEvent carries no timestamp — only
// the in-memory Event does — so the journal stamps its own.
type JournalEntry struct {
	Time        time.Time     `json:"time"`
	Action      string        `json:"action"`
	Protocol    string        `json:"protocol,omitempty"`
	ErrorKind   string        `json:"error_kind,omitempty"`
	Detail      string        `json:"detail,omitempty"`
	Duration    time.Duration `json:"duration,omitempty"`
	OperationID string        `json:"operation_id,omitempty"`
	Profile     string        `json:"profile,omitempty"`
	Backend     string        `json:"backend,omitempty"`
	State       string        `json:"state,omitempty"`
	Recovery    string        `json:"recovery,omitempty"`
}

// FileJournal persists failed operations as JSON Lines.
//
// EventStore keeps the full event history, but in memory and capped, so it dies
// with the daemon — leaving nothing to answer "what went wrong before this
// crash", which is exactly what an operator asks after one. The journal keeps
// only failures, so the file stays small enough to read whole, and a diagnostic
// reads it directly rather than through the daemon that may not be running.
type FileJournal struct {
	path  string
	limit int
	now   func() time.Time
	mu    sync.Mutex
}

// NewFileJournal returns a journal writing to path. A limit of zero means
// DefaultJournalLimit.
func NewFileJournal(path string, limit int) *FileJournal {
	if limit <= 0 {
		limit = DefaultJournalLimit
	}
	return &FileJournal{path: path, limit: limit, now: time.Now}
}

// Record appends a failed event. Successes are not persisted: they are the
// overwhelming majority and would push the failures out of a bounded file.
//
// A journal write must never take down the operation it is recording, so
// errors here are dropped rather than propagated — AuditSink cannot report them
// anyway.
func (j *FileJournal) Record(_ context.Context, event AuditEvent) {
	if event.Success {
		return
	}
	entry := JournalEntry{
		Time: j.now().UTC(), Action: event.Action, Protocol: event.Protocol,
		ErrorKind: string(event.ErrorKind), Detail: event.Detail, Duration: event.Duration,
		OperationID: event.OperationID, Profile: event.Profile, Backend: event.Backend,
		State: string(event.State), Recovery: string(event.Recovery),
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(j.path), 0o700); err != nil {
		return
	}
	file, err := os.OpenFile(j.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, writeErr := file.Write(append(line, '\n'))
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return
	}
	j.compactLocked()
}

// Recent returns the most recently recorded failures, newest last. A missing
// journal is an empty result, not an error: nothing has failed yet.
func (j *FileJournal) Recent(limit int) ([]JournalEntry, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	entries, err := j.readLocked()
	if err != nil || limit <= 0 || len(entries) <= limit {
		return entries, err
	}
	return entries[len(entries)-limit:], nil
}

func (j *FileJournal) readLocked() ([]JournalEntry, error) {
	file, err := os.Open(j.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var entries []JournalEntry
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		var entry JournalEntry
		// A truncated final line from an interrupted write is skipped rather
		// than failing the read: partial history beats none.
		if err := json.Unmarshal(scanner.Bytes(), &entry); err == nil {
			entries = append(entries, entry)
		}
	}
	return entries, scanner.Err()
}

// compactLocked rewrites the file with only the newest entries once it grows
// past the limit, so an append-only journal cannot grow without bound.
func (j *FileJournal) compactLocked() {
	entries, err := j.readLocked()
	if err != nil || len(entries) <= j.limit {
		return
	}
	var buf []byte
	for _, entry := range entries[len(entries)-j.limit:] {
		line, err := json.Marshal(entry)
		if err != nil {
			return
		}
		buf = append(append(buf, line...), '\n')
	}
	_ = os.WriteFile(j.path, buf, 0o600)
}
