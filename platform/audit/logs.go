package audit

import (
	"bytes"
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
	path, err := archivePath(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
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
