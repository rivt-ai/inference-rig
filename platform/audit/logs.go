package audit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"syscall"
	"time"

	"inferencerig/config"
)

// followPollInterval bounds how stale a followed log can be: a line appended to
// the file surfaces to the caller at most one interval later. Polling keeps
// FollowLog portable across the platforms we support without an fsnotify
// dependency, at the cost of that latency ceiling.
const followPollInterval = 500 * time.Millisecond

const MaxTailLines, maxTailReadBytes = 5000, 10 * 1024 * 1024

var archiveNamePattern, logNamePattern = regexp.MustCompile(`^([A-Za-z0-9_-]+)-(\d{8}T\d{6}\.\d{9}Z)\.log$`), regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type Archive struct {
	ID         string    `json:"id"`
	Service    string    `json:"service"`
	SizeBytes  int64     `json:"size_bytes"`
	ArchivedAt time.Time `json:"archived_at"`
}

func AttachLogs(cmd *exec.Cmd, name string) (func(), error) {
	logPath, err := GetLogPath(name)
	if err != nil {
		return nil, err
	}
	_, _ = ArchiveLog(name, time.Now().UTC())
	logFile, err := initLogFile(logPath)
	if err != nil {
		return nil, err
	}

	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	return func() {
		_ = logFile.Close()
	}, nil
}

func GetArchiveDir() (string, error) {
	home, err := config.Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "logs", "archive"), nil
}

func ArchiveLog(name string, now time.Time) (string, error) {
	if !logNamePattern.MatchString(name) {
		return "", fmt.Errorf("invalid log name %q", name)
	}
	path, err := GetLogPath(name)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("log path %q is not a regular file", path)
	}
	if info.Size() == 0 {
		_ = os.Remove(path)
		return "", nil
	}
	dir, err := GetArchiveDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	id := fmt.Sprintf("%s-%s.log", name, now.UTC().Format("20060102T150405.000000000Z"))
	target := filepath.Join(dir, id)
	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("log archive %q already exists", id)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := moveLog(path, target); err != nil {
		return "", err
	}
	return id, nil
}

func moveLog(source, target string) error {
	err := os.Rename(source, target)
	var linkErr *os.LinkError
	if err == nil || !errors.As(err, &linkErr) {
		return err
	}
	if linkErr.Err == syscall.EXDEV {
		return copyAndDelete(source, target)
	}
	return err
}

func copyAndDelete(source, target string) error {
	// src is read-only, so its close error carries no data-loss signal and the
	// deferred close needs no bookkeeping to stay correct.
	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = os.Remove(target)
		return err
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(target)
		return err
	}
	return os.Remove(source)
}

func TailLogLines(name string, lines int) (string, error) {
	if !logNamePattern.MatchString(name) {
		return "", fmt.Errorf("invalid log name %q", name)
	}
	path, err := GetLogPath(name)
	if err != nil {
		return "", err
	}
	return tailFileLines(path, lines)
}

// ValidLogName reports whether name is a usable service log name. Callers that
// must distinguish a malformed name from a missing log check this first, since
// the tail and follow helpers collapse both into one error.
func ValidLogName(name string) bool { return logNamePattern.MatchString(name) }

// ValidArchiveID reports whether id names an archive without escaping the
// archive directory.
func ValidArchiveID(id string) bool {
	return filepath.Base(id) == id && archiveNamePattern.MatchString(id)
}

// ArchiveService extracts the service an archive belongs to from its ID.
// Service names may themselves contain hyphens, so only the pattern can split
// the ID correctly. An unparsable ID yields an empty string.
func ArchiveService(id string) string {
	match := archiveNamePattern.FindStringSubmatch(id)
	if match == nil {
		return ""
	}
	return match[1]
}

// LogExists reports whether a service currently has a log file. A path that
// exists but is not a regular file counts as absent, matching ArchiveLog's
// refusal to treat symlinks and devices as logs.
func LogExists(name string) (bool, error) {
	if !ValidLogName(name) {
		return false, fmt.Errorf("invalid log name %q", name)
	}
	path, err := GetLogPath(name)
	if err != nil {
		return false, err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.Mode().IsRegular(), nil
}

// FollowLog streams lines appended to a service log to emit until ctx is
// cancelled or emit fails. It starts at the current end of the file, so only
// new output is delivered, and it recovers from the rotation ArchiveLog
// performs by resuming at the replacement file's start.
func FollowLog(ctx context.Context, name string, emit func(string) error) error {
	if !ValidLogName(name) {
		return fmt.Errorf("invalid log name %q", name)
	}
	path, err := GetLogPath(name)
	if err != nil {
		return err
	}
	offset, err := logEnd(path)
	if err != nil {
		return err
	}
	var partial []byte
	ticker := time.NewTicker(followPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		var lines []string
		lines, offset, partial, err = readNewLines(path, offset, partial)
		if err != nil {
			return err
		}
		for _, line := range lines {
			if err := emit(line); err != nil {
				return err
			}
		}
	}
}

func logEnd(path string) (int64, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// readNewLines returns the complete lines written past offset, along with the
// new offset and any trailing partial line to prepend on the next poll.
func readNewLines(path string, offset int64, partial []byte) ([]string, int64, []byte, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		// The log was archived away; the replacement starts empty.
		return nil, 0, nil, nil
	}
	if err != nil {
		return nil, offset, partial, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, offset, partial, err
	}
	if info.Size() < offset {
		offset, partial = 0, nil
	}
	// A burst larger than the tail budget is skipped rather than buffered, so a
	// runaway writer cannot grow this read without bound.
	if info.Size()-offset > maxTailReadBytes {
		offset, partial = info.Size()-maxTailReadBytes, nil
	}
	if info.Size() == offset {
		return nil, offset, partial, nil
	}
	chunk := make([]byte, info.Size()-offset)
	n, err := file.ReadAt(chunk, offset)
	if err != nil && err != io.EOF {
		return nil, offset, partial, err
	}
	data := slices.Concat(partial, chunk[:n])
	offset += int64(n)
	var lines []string
	for {
		index := bytes.IndexByte(data, '\n')
		if index < 0 {
			break
		}
		lines = append(lines, string(data[:index]))
		data = data[index+1:]
	}
	return lines, offset, bytes.Clone(data), nil
}

// tailFile returns the formatted tail of path, opening the file once so the
// read and the size it is bounded by share a single view of it. A missing file
// yields an empty tail and no error.
func tailFile(path string, lines int) (string, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	data, err := readTailChunks(file, info.Size(), lines)
	if err != nil {
		return "", err
	}
	return formatTail(data, lines), nil
}

func ListArchives() ([]Archive, error) {
	dir, err := GetArchiveDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []Archive{}, nil
	}
	if err != nil {
		return nil, err
	}
	archives := make([]Archive, 0, len(entries))
	for _, entry := range entries {
		archive, ok := archiveEntry(entry)
		if ok {
			archives = append(archives, archive)
		}
	}
	slices.SortFunc(archives, func(a, b Archive) int { return b.ArchivedAt.Compare(a.ArchivedAt) })
	return archives, nil
}

func TailArchive(id string, lines int) (string, error) {
	path, err := archivePath(id)
	if err != nil {
		return "", err
	}
	return tailFileLines(path, lines)
}

func DeleteArchive(id string) error {
	_, err := RemoveArchive(id)
	return err
}

// RemoveArchive deletes a single archive and reports whether it existed, so
// callers can answer an unknown ID with a not-found status instead of a
// silent success.
func RemoveArchive(id string) (bool, error) {
	path, err := archivePath(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ClearArchives deletes every archive regardless of retention and returns how
// many were removed.
func ClearArchives() (int, error) {
	archives, err := ListArchives()
	if err != nil {
		return 0, err
	}
	return deleteArchives(archives)
}

func CleanupArchives(retention time.Duration, now time.Time) (int, error) {
	if retention == 0 {
		return 0, nil
	}
	archives, err := ListArchives()
	if err != nil {
		return 0, err
	}
	cutoff := now.Add(-retention)
	expired := make([]Archive, 0, len(archives))
	for _, archive := range archives {
		if archive.ArchivedAt.Before(cutoff) {
			expired = append(expired, archive)
		}
	}
	return deleteArchives(expired)
}

func deleteArchives(archives []Archive) (int, error) {
	deleted := 0
	var errs error
	for _, archive := range archives {
		if err := DeleteArchive(archive.ID); err != nil {
			errs = errors.Join(errs, err)
			continue
		}
		deleted++
	}
	return deleted, errs
}

func archiveEntry(entry os.DirEntry) (Archive, bool) {
	if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
		return Archive{}, false
	}
	match := archiveNamePattern.FindStringSubmatch(entry.Name())
	if match == nil {
		return Archive{}, false
	}
	archivedAt, err := time.Parse("20060102T150405.000000000Z", match[2])
	if err != nil {
		return Archive{}, false
	}
	info, err := entry.Info()
	if err != nil {
		return Archive{}, false
	}
	return Archive{ID: entry.Name(), Service: match[1], SizeBytes: info.Size(), ArchivedAt: archivedAt}, true
}

func archivePath(id string) (string, error) {
	if filepath.Base(id) != id || archiveNamePattern.FindStringSubmatch(id) == nil {
		return "", fmt.Errorf("invalid log archive %q", id)
	}
	dir, err := GetArchiveDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, id)
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("log archive %q is not a regular file", id)
	}
	return path, nil
}

func tailFileLines(path string, lines int) (string, error) {
	if lines < 1 || lines > MaxTailLines {
		return "", fmt.Errorf("log lines must be between 1 and %d", MaxTailLines)
	}
	return tailFile(path, lines)
}

func readTailChunks(file *os.File, end int64, lines int) ([]byte, error) {
	const chunkSize int64 = 64 * 1024
	var chunks [][]byte
	totalBytes, newlines := 0, 0
	for end > 0 && totalBytes < maxTailReadBytes && newlines < lines {
		readSize := min(chunkSize, int64(maxTailReadBytes-totalBytes))
		start := max(int64(0), end-readSize)
		chunk := make([]byte, end-start)
		n, err := file.ReadAt(chunk, start)
		if err != nil && err != io.EOF {
			return nil, err
		}
		chunk = chunk[:n]
		chunks = append(chunks, chunk)
		totalBytes += len(chunk)
		newlines += bytes.Count(chunk, []byte{'\n'})
		end = start
	}
	slices.Reverse(chunks)
	return slices.Concat(chunks...), nil
}

func formatTail(data []byte, lines int) string {
	trailingNewline := len(data) > 0 && data[len(data)-1] == '\n'
	parts := bytes.Split(data, []byte{'\n'})
	if trailingNewline {
		parts = parts[:len(parts)-1]
	}
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	result := bytes.Join(parts, []byte{'\n'})
	if trailingNewline && len(result) > 0 {
		result = append(result, '\n')
	}
	return string(result)
}

func initLogFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}

	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}

func GetLogPath(name string) (string, error) {
	home, err := config.Home()
	if err != nil {
		return "", err
	}
	runDir := filepath.Join(home, "run")
	return filepath.Join(runDir, name+".log"), nil
}
