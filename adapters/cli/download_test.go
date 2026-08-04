package cli

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/internal/style"
)

func testModel(job *controlv1.ModelDownload) *downloadModel {
	m := newDownloadModel(context.Background(), nil, job)
	// Colour off so the assertions below are about content, not escapes.
	m.paint = style.Plain
	return m
}

// The bar has to show the three things an operator is waiting on: which
// download this is, how far along it is, and how much has moved.
func TestDownloadViewShowsProgress(t *testing.T) {
	view := testModel(&controlv1.ModelDownload{
		Id: "dl_1", Profile: "smol", State: "running",
		ReceivedBytes: 3 * 1024 * 1024 * 1024, TotalBytes: 6 * 1024 * 1024 * 1024, Percent: 50,
	}).View().Content
	for _, want := range []string{"smol", "50.0%", "3.0 GiB / 6.0 GiB", "ctrl-c to cancel"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	// A half-full meter must show both a filled and an empty cell, which is
	// the part that silently degrades to a blank line if the meter is
	// misconfigured.
	if !strings.Contains(view, "█") || !strings.Contains(view, "░") {
		t.Errorf("meter did not render a bar:\n%q", view)
	}
}

// Before the daemon knows the total (a multi-file plan fetches manifests
// first), showing "3.0 GiB / 0 B" would be worse than showing nothing.
func TestDownloadViewOmitsUnknownTotal(t *testing.T) {
	view := testModel(&controlv1.ModelDownload{
		Id: "dl_1", State: "running", ReceivedBytes: 1024,
	}).View().Content
	if strings.Contains(view, "/") {
		t.Errorf("view showed a total it does not have:\n%s", view)
	}
	if !strings.Contains(view, "1.0 KiB") {
		t.Errorf("view missing the received count:\n%s", view)
	}
	// No total means no percentage to compute, so a determinate bar would sit
	// at 0% for the whole transfer and read as a stall.
	if strings.Contains(view, "░") || strings.Contains(view, "0.0%") {
		t.Errorf("view drew a determinate bar with no total to divide by:\n%s", view)
	}
	// With no profile set the id has to stand in, or the line names nothing.
	if !strings.Contains(view, "dl_1") {
		t.Errorf("view names neither profile nor id:\n%s", view)
	}
}

// ctrl-c must reach the daemon, not just close the UI. A second press quits
// even if the daemon never answers the first.
func TestDownloadCancelIsIdempotentThenQuits(t *testing.T) {
	m := testModel(&controlv1.ModelDownload{Id: "dl_1", State: "running"})
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); cmd == nil {
		t.Fatal("first ctrl-c produced no command")
	}
	if !m.cancelling {
		t.Fatal("first ctrl-c did not mark the download as cancelling")
	}
	if !strings.Contains(m.View().Content, "cancelling") {
		t.Errorf("view does not show the cancel is in flight:\n%s", m.View().Content)
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("second ctrl-c produced no command")
	}
	if msg := cmd(); msg != tea.Quit() {
		t.Errorf("second ctrl-c should quit, got %T", msg)
	}
}

// A terminal state must stop the loop; without this the bar spins forever on a
// finished download.
func TestDownloadQuitsOnTerminalState(t *testing.T) {
	m := testModel(&controlv1.ModelDownload{Id: "dl_1", State: "running"})
	_, cmd := m.Update(downloadStatusMsg{job: &controlv1.ModelDownload{Id: "dl_1", State: "completed"}})
	if cmd == nil {
		t.Fatal("completed download produced no command")
	}
	if msg := cmd(); msg != tea.Quit() {
		t.Errorf("completed download should quit, got %T", msg)
	}
}

func TestIsTerminalStateMatchesTheWireSpelling(t *testing.T) {
	// already_downloaded is the spelling that actually crosses the wire; the
	// TUI compared against a hyphenated version and never matched.
	for _, state := range []string{"completed", "failed", "cancelled", "already_downloaded"} {
		if !isTerminalState(state) {
			t.Errorf("%q should be terminal", state)
		}
	}
	for _, state := range []string{"queued", "running", ""} {
		if isTerminalState(state) {
			t.Errorf("%q should not be terminal", state)
		}
	}
}
