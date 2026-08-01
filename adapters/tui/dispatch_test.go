package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

// runRPC is where a keypress becomes a real control-plane call. The rendering
// around it is presentation, but this mapping is not: a swapped case here stops
// the wrong profile, and nothing on screen would say so.

type dispatchClient struct {
	controlv1connect.ControlServiceClient
	calls []string
	err   error
}

func (c *dispatchClient) record(name string) error {
	c.calls = append(c.calls, name)
	return c.err
}

func (c *dispatchClient) StartRuntime(_ context.Context, r *controlv1.StartRuntimeRequest) (*controlv1.StartRuntimeResponse, error) {
	return &controlv1.StartRuntimeResponse{}, c.record(fmt.Sprintf("start:%s:%t", r.GetProfile(), r.GetReplace()))
}

func (c *dispatchClient) ResetRuntimes(context.Context, *controlv1.ResetRuntimesRequest) (*controlv1.ResetRuntimesResponse, error) {
	return &controlv1.ResetRuntimesResponse{}, c.record("reset")
}

func (c *dispatchClient) StopRuntime(_ context.Context, r *controlv1.StopRuntimeRequest) (*controlv1.StopRuntimeResponse, error) {
	return &controlv1.StopRuntimeResponse{}, c.record("stop:" + r.GetProfile())
}

func (c *dispatchClient) RestartRuntime(_ context.Context, r *controlv1.RestartRuntimeRequest) (*controlv1.RestartRuntimeResponse, error) {
	return &controlv1.RestartRuntimeResponse{}, c.record("restart:" + r.GetProfile())
}

func (c *dispatchClient) StartModelDownload(_ context.Context, r *controlv1.StartModelDownloadRequest) (*controlv1.StartModelDownloadResponse, error) {
	return &controlv1.StartModelDownloadResponse{Download: &controlv1.ModelDownload{Id: "job-1", State: "running"}},
		c.record("download:" + r.GetProfile())
}

func (c *dispatchClient) CancelModelDownload(_ context.Context, r *controlv1.CancelModelDownloadRequest) (*controlv1.CancelModelDownloadResponse, error) {
	return &controlv1.CancelModelDownloadResponse{Download: &controlv1.ModelDownload{Id: r.GetId(), State: "cancelled"}},
		c.record("cancel:" + r.GetId())
}

func (c *dispatchClient) DeleteLocalModel(_ context.Context, r *controlv1.DeleteLocalModelRequest) (*controlv1.DeleteLocalModelResponse, error) {
	return &controlv1.DeleteLocalModelResponse{}, c.record("delete:" + r.GetBackend() + ":" + r.GetPath())
}

func (c *dispatchClient) SetProfileAutostart(_ context.Context, r *controlv1.SetProfileAutostartRequest) (*controlv1.SetProfileAutostartResponse, error) {
	return &controlv1.SetProfileAutostartResponse{}, c.record("autostart:" + r.GetName())
}

func (c *dispatchClient) CleanupProfile(_ context.Context, r *controlv1.CleanupProfileRequest) (*controlv1.CleanupProfileResponse, error) {
	return &controlv1.CleanupProfileResponse{}, c.record("cleanup:" + r.GetName())
}

func (c *dispatchClient) InstallBackend(_ context.Context, r *controlv1.InstallBackendRequest) (*controlv1.InstallBackendResponse, error) {
	return &controlv1.InstallBackendResponse{}, c.record("install:" + r.GetBackend())
}

func (c *dispatchClient) ApplyDownloadToProfile(_ context.Context, r *controlv1.ApplyDownloadToProfileRequest) (*controlv1.ApplyDownloadToProfileResponse, error) {
	return &controlv1.ApplyDownloadToProfileResponse{}, c.record("apply:" + r.GetProfile() + ":" + r.GetId())
}

func newDispatchDashboard(client controlv1connect.ControlServiceClient) *dashboard {
	return &dashboard{
		ctx: context.Background(), client: client, manage: true,
		data: snapshot{warnings: map[string]string{}},
	}
}

func TestRunRPCDispatchesEveryActionToItsProcedure(t *testing.T) {
	tests := []struct {
		name    string
		request rpcRequest
		want    string
	}{
		{"start", rpcRequest{kind: rpcStart, profile: "demo", replace: true}, "start:demo:true"},
		{"reset", rpcRequest{kind: rpcReset}, "reset"},
		{"stop", rpcRequest{kind: rpcStop, profile: "demo"}, "stop:demo"},
		{"restart", rpcRequest{kind: rpcRestart, profile: "demo"}, "restart:demo"},
		{"autostart", rpcRequest{kind: rpcAutostart, profile: "demo", enabled: true}, "autostart:demo"},
		{"download", rpcRequest{kind: rpcDownload, profile: "demo"}, "download:demo"},
		{"cancel", rpcRequest{kind: rpcCancel, id: "job-1"}, "cancel:job-1"},
		{"apply", rpcRequest{kind: rpcApply, profile: "demo", id: "job-1"}, "apply:demo:job-1"},
		{"cleanup", rpcRequest{kind: rpcCleanup, profile: "demo"}, "cleanup:demo"},
		{"delete", rpcRequest{kind: rpcDelete, backend: "one", path: "/model"}, "delete:one:/model"},
		{"install", rpcRequest{kind: rpcInstall, backend: "one"}, "install:one"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &dispatchClient{}
			msg := newDispatchDashboard(client).runRPC(test.request)().(actionMsg)
			if msg.err != nil {
				t.Fatalf("%s failed: %v", test.name, msg.err)
			}
			if len(client.calls) != 1 || client.calls[0] != test.want {
				t.Fatalf("calls = %v, want [%s]", client.calls, test.want)
			}
		})
	}
}

// A download action carries the job back so the table updates without waiting
// for the next poll; losing it makes the UI look like nothing happened.
func TestRunRPCCarriesTheDownloadBack(t *testing.T) {
	client := &dispatchClient{}
	started := newDispatchDashboard(client).runRPC(rpcRequest{kind: rpcDownload, profile: "demo"})().(actionMsg)
	if started.download.GetId() != "job-1" {
		t.Fatalf("started download = %#v", started.download)
	}
	cancelled := newDispatchDashboard(client).runRPC(rpcRequest{kind: rpcCancel, id: "job-1"})().(actionMsg)
	if cancelled.download.GetState() != "cancelled" {
		t.Fatalf("cancelled download = %#v", cancelled.download)
	}
}

var errDispatch = errors.New("daemon refused")

func TestRunRPCReportsFailureWithoutLosingTheNotice(t *testing.T) {
	client := &dispatchClient{err: errDispatch}
	msg := newDispatchDashboard(client).runRPC(rpcRequest{kind: rpcStop, profile: "demo", notice: "runtime stopped"})().(actionMsg)
	if !errors.Is(msg.err, errDispatch) {
		t.Fatalf("error = %v, want %v", msg.err, errDispatch)
	}
}

// updateAction is what turns the completed call into what the operator sees. A
// failure has to surface as a warning, and a later success has to clear it —
// a stuck warning is indistinguishable from a still-broken daemon.
func TestUpdateActionSurfacesThenClearsFailures(t *testing.T) {
	app := newDispatchDashboard(&dispatchClient{})
	app.updateAction(actionMsg{err: errDispatch})
	if app.data.warnings["action"] == "" {
		t.Fatal("a failed action produced no warning")
	}
	if app.notice != "" {
		t.Errorf("a failed action left a success notice: %q", app.notice)
	}

	app.updateAction(actionMsg{notice: "runtime started", download: &controlv1.ModelDownload{Id: "job-1", State: "running"}})
	if app.data.warnings["action"] != "" {
		t.Errorf("a successful action left the previous warning: %q", app.data.warnings["action"])
	}
	if app.notice != "runtime started" {
		t.Errorf("notice = %q", app.notice)
	}
	if len(app.data.downloads) != 1 {
		t.Fatalf("downloads = %#v", app.data.downloads)
	}
}

// The downloads table is keyed by job ID: a progress update must replace the
// row rather than append a duplicate.
func TestUpsertDownloadReplacesByID(t *testing.T) {
	app := newDispatchDashboard(&dispatchClient{})
	app.upsertDownload(&controlv1.ModelDownload{Id: "job-1", State: "queued"})
	app.upsertDownload(&controlv1.ModelDownload{Id: "job-1", State: "running"})
	app.upsertDownload(&controlv1.ModelDownload{Id: "job-2", State: "queued"})
	if len(app.data.downloads) != 2 {
		t.Fatalf("downloads = %#v, want two rows", app.data.downloads)
	}
	if app.data.downloads[0].GetState() != "running" {
		t.Errorf("job-1 was not updated in place: %#v", app.data.downloads[0])
	}
}

// The keys that start a runtime and cancel a download are the ones an operator
// reaches for under pressure, so the bindings are pinned here rather than left
// to the rendering tests.
func TestModelsPageKeysRequestDownloadAndCancel(t *testing.T) {
	page := newModelsPage()
	data := snapshot{
		backends:  &controlv1.ListBackendsResponse{Backends: []*controlv1.BackendInfo{{Name: "one"}}},
		profiles:  &controlv1.ListProfilesResponse{Profiles: []*controlv1.Profile{{Name: "demo", Backend: "one"}}},
		downloads: []*controlv1.ModelDownload{{Id: "job-1", State: "running", Profile: "demo"}},
	}
	page.active = paneDownloads
	if cmd := page.Update(keyMsg("c"), data); cmd != nil {
		request, ok := cmd().(rpcRequest)
		if !ok || request.kind != rpcCancel || request.id != "job-1" {
			t.Fatalf("cancel request = %#v (ok=%v)", request, ok)
		}
	} else {
		t.Fatal("the cancel key produced no request for a running download")
	}
}

func TestProfilesOnAnotherBackendOfferResetInline(t *testing.T) {
	page := newModelsPage()
	data := snapshot{info: &controlv1.GetInfoResponse{ActiveBackend: "backend-a"}, profiles: &controlv1.ListProfilesResponse{Profiles: []*controlv1.Profile{{Name: "coder", Backend: "backend-b"}}}}
	if cmd := page.Update(keyMsg("enter"), data); cmd != nil {
		t.Fatal("the first Enter reset without confirmation")
	}
	if rows := page.statusRows("", data); !strings.Contains(rows, "backend-a is active — reset to start backend-b profiles") {
		t.Fatalf("inline reset reason missing from %q", rows)
	}
	if request := page.Update(keyMsg("enter"), data)().(rpcRequest); request.kind != rpcReset {
		t.Fatalf("request = %#v, want reset", request)
	}
}

func TestExclusiveBackendStartConfirmsThenReplaces(t *testing.T) {
	page := newModelsPage()
	data := snapshot{
		info:     &controlv1.GetInfoResponse{ActiveBackend: "backend-a", RunningProfiles: []string{"chat"}},
		backends: &controlv1.ListBackendsResponse{Backends: []*controlv1.BackendInfo{{Name: "backend-a", Capabilities: &controlv1.BackendCapabilities{SingleActiveProfile: true}}}},
		profiles: &controlv1.ListProfilesResponse{Profiles: []*controlv1.Profile{{Name: "coder", Backend: "backend-a"}, {Name: "chat", Backend: "backend-a"}}},
	}
	if cmd := page.Update(keyMsg("enter"), data); cmd != nil {
		t.Fatal("the first Enter replaced without confirmation")
	}
	request := page.Update(keyMsg("enter"), data)().(rpcRequest)
	if request.kind != rpcStart || !request.replace {
		t.Fatalf("request = %#v, want replacing start", request)
	}
}

func TestProfileRowsShowTransitionalRuntimeStates(t *testing.T) {
	page := newModelsPage()
	data := snapshot{
		profiles: &controlv1.ListProfilesResponse{Profiles: []*controlv1.Profile{{Name: "coder", Backend: "backend-a"}}},
		runtimes: &controlv1.GetRuntimeStatusResponse{Profiles: []*controlv1.ProfileRuntimeStatus{{Name: "coder", Status: &controlv1.RuntimeStatus{State: "activating"}}}},
	}
	page.setRows(data)
	if got := page.profiles.Rows()[0][3]; got != "Activating" {
		t.Fatalf("state = %q, want Activating", got)
	}
}

// A dashboard with nothing to act on must produce no command at all; an
// accidental nil-dereference here would take the whole TUI down.
func TestUpdateActionIgnoresUnknownMessages(t *testing.T) {
	app := newDispatchDashboard(&dispatchClient{})
	if cmd := app.updateAction(tea.WindowSizeMsg{Width: 80, Height: 24}); cmd != nil {
		t.Fatalf("unknown message produced a command: %v", cmd)
	}
}
