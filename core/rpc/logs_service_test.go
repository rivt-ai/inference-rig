package rpc

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"inferencerig/config"
	"inferencerig/core/control"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
	"inferencerig/platform/audit"
)

// logsHome points the audit package at a scratch home and returns a service
// with no manager, which the logs RPCs never touch.
func logsHome(t *testing.T) *ControlService {
	t.Helper()
	t.Setenv(config.ProjectHomeEnv, t.TempDir())
	return &ControlService{}
}

func writeServiceLog(t *testing.T, name, content string) string {
	t.Helper()
	path, err := audit.GetLogPath(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeArchive(t *testing.T, id, content string) {
	t.Helper()
	dir, err := audit.GetArchiveDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTailLinesClamping(t *testing.T) {
	cases := []struct {
		name  string
		lines int32
		want  int
	}{
		{name: "zero defaults", lines: 0, want: defaultLogLines},
		{name: "negative defaults", lines: -20, want: defaultLogLines},
		{name: "passthrough", lines: 42, want: 42},
		{name: "at maximum", lines: audit.MaxTailLines, want: audit.MaxTailLines},
		{name: "clamped to maximum", lines: audit.MaxTailLines * 10, want: audit.MaxTailLines},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := tailLines(testCase.lines); got != testCase.want {
				t.Fatalf("tailLines(%d) = %d, want %d", testCase.lines, got, testCase.want)
			}
		})
	}
}

func TestGetLogsTail(t *testing.T) {
	cases := []struct {
		name  string
		lines int32
		want  string
	}{
		{name: "fewer lines than requested", lines: 10, want: "one\ntwo\nthree\n"},
		{name: "tail truncates", lines: 2, want: "two\nthree\n"},
		{name: "unset returns whole log", lines: 0, want: "one\ntwo\nthree\n"},
		{name: "oversized request is clamped not rejected", lines: audit.MaxTailLines * 10, want: "one\ntwo\nthree\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service := logsHome(t)
			writeServiceLog(t, "control", "one\ntwo\nthree\n")

			resp, err := service.GetLogs(t.Context(), &controlv1.GetLogsRequest{
				Service: "control", Lines: testCase.lines,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !resp.GetOk() || resp.GetService() != "control" {
				t.Fatalf("ok = %v, service = %q", resp.GetOk(), resp.GetService())
			}
			if resp.GetText() != testCase.want {
				t.Fatalf("text = %q, want %q", resp.GetText(), testCase.want)
			}
		})
	}
}

func TestLogsRejectPathTraversal(t *testing.T) {
	// A file outside the log tree that a successful traversal would expose.
	secretDir := t.TempDir()
	secret := filepath.Join(secretDir, "secret.log")
	if err := os.WriteFile(secret, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	names := []string{
		"..",
		"../../etc/passwd",
		"control/../../../etc/passwd",
		"/etc/passwd",
		secret,
		"control\x00",
		"control.log",
		"",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			service := logsHome(t)
			for method, err := range map[string]error{
				"GetLogs": func() error {
					_, err := service.GetLogs(t.Context(), &controlv1.GetLogsRequest{Service: name})
					return err
				}(),
				"GetLogArchive": func() error {
					_, err := service.GetLogArchive(t.Context(), &controlv1.GetLogArchiveRequest{Id: name})
					return err
				}(),
				"DeleteLogArchive": func() error {
					_, err := service.DeleteLogArchive(t.Context(), &controlv1.DeleteLogArchiveRequest{Id: name})
					return err
				}(),
				"WatchLogs": service.WatchLogs(t.Context(), &controlv1.WatchLogsRequest{Service: name}, nil),
			} {
				if kind := ErrorKindFromRPC(err); kind != control.ErrorInvalidInput {
					t.Fatalf("%s(%q) kind = %q, want %q", method, name, kind, control.ErrorInvalidInput)
				}
			}
			if _, err := os.Stat(secret); err != nil {
				t.Fatalf("file outside the log tree was touched: %v", err)
			}
		})
	}
}

func TestLogsUnknownTargetsAreNotFound(t *testing.T) {
	const missingArchive = "ghost-20260101T000000.000000000Z.log"
	cases := []struct {
		name string
		call func(*ControlService) error
	}{
		{name: "unknown service", call: func(s *ControlService) error {
			_, err := s.GetLogs(t.Context(), &controlv1.GetLogsRequest{Service: "ghost"})
			return err
		}},
		{name: "unknown archive tail", call: func(s *ControlService) error {
			_, err := s.GetLogArchive(t.Context(), &controlv1.GetLogArchiveRequest{Id: missingArchive})
			return err
		}},
		{name: "unknown archive delete", call: func(s *ControlService) error {
			_, err := s.DeleteLogArchive(t.Context(), &controlv1.DeleteLogArchiveRequest{Id: missingArchive})
			return err
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service := logsHome(t)
			err := testCase.call(service)
			if err == nil {
				t.Fatal("expected an error")
			}
			if kind := ErrorKindFromRPC(err); kind != control.ErrorNotFound {
				t.Fatalf("kind = %q, want %q", kind, control.ErrorNotFound)
			}
		})
	}
}

// archiveFixture seeds three archives across two services and returns the
// service plus their ids, newest last.
func archiveFixture(t *testing.T) (*ControlService, []string) {
	t.Helper()
	service := logsHome(t)
	ids := []string{
		"control-20260101T000000.000000000Z.log",
		"control-20260102T000000.000000000Z.log",
		"web-20260103T000000.000000000Z.log",
	}
	for _, id := range ids {
		writeArchive(t, id, "alpha\nbeta\n")
	}
	return service, ids
}

func TestLogArchiveListAndRead(t *testing.T) {
	service, ids := archiveFixture(t)

	listed, err := service.ListLogArchives(t.Context(), &controlv1.ListLogArchivesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.GetArchives()) != len(ids) {
		t.Fatalf("archives = %d, want %d", len(listed.GetArchives()), len(ids))
	}
	// ListArchives sorts newest first, so the web archive leads.
	if first := listed.GetArchives()[0]; first.GetService() != "web" || first.GetSizeBytes() != 11 {
		t.Fatalf("first archive = %+v", first)
	}

	tail, err := service.GetLogArchive(t.Context(), &controlv1.GetLogArchiveRequest{Id: ids[0], Lines: 1})
	if err != nil {
		t.Fatal(err)
	}
	if tail.GetService() != "control" || tail.GetText() != "beta\n" {
		t.Fatalf("service = %q, text = %q", tail.GetService(), tail.GetText())
	}
}

func TestLogArchiveDeleteAndClear(t *testing.T) {
	service, ids := archiveFixture(t)

	deleted, err := service.DeleteLogArchive(t.Context(), &controlv1.DeleteLogArchiveRequest{Id: ids[0]})
	if err != nil {
		t.Fatal(err)
	}
	if deleted.GetDeleted() != 1 {
		t.Fatalf("deleted = %d, want 1", deleted.GetDeleted())
	}

	cleared, err := service.ClearLogArchives(t.Context(), &controlv1.ClearLogArchivesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.GetDeleted() != 2 {
		t.Fatalf("cleared = %d, want 2", cleared.GetDeleted())
	}
	empty, err := service.ListLogArchives(t.Context(), &controlv1.ListLogArchivesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.GetArchives()) != 0 {
		t.Fatalf("archives after clear = %d", len(empty.GetArchives()))
	}
}

func TestWatchLogsStreamsUntilCancel(t *testing.T) {
	logsHome(t)
	path := writeServiceLog(t, "control", "existing\n")

	_, handler := ControlHandler(&ControlService{})
	server := httptest.NewServer(handler)
	defer server.Close()
	client := controlv1connect.NewControlServiceClient(server.Client(), server.URL)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// FollowLog starts at end-of-file, so keep appending until the handler has
	// snapshotted its offset and picked a line up. This has to run before the
	// call: connect withholds the response headers until the first message, so
	// WatchLogs itself blocks until a line exists to send.
	appending := make(chan struct{})
	go func() {
		defer close(appending)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				return
			}
			_, _ = file.WriteString("fresh\n")
			_ = file.Close()
			time.Sleep(50 * time.Millisecond)
		}
	}()

	stream, err := client.WatchLogs(ctx, &controlv1.WatchLogsRequest{Service: "control"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()

	if !stream.Receive() {
		t.Fatalf("expected a line, got %v", stream.Err())
	}
	if msg := stream.Msg(); msg.GetService() != "control" || msg.GetLine() != "fresh" {
		t.Fatalf("message = %+v", msg)
	}

	cancel()
	<-appending
	for stream.Receive() {
		// Drain whatever was already in flight before cancellation landed.
	}
	if err := stream.Err(); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("stream error after cancel = %v", err)
	}
}
