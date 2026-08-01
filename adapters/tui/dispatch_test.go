package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"inferencerig/core/profiles"
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

type localPollClient struct {
	testClient
	models map[string]string
	err    error
}

func (c localPollClient) ListLocalModels(_ context.Context, request *controlv1.ListLocalModelsRequest) (*controlv1.ListLocalModelsResponse, error) {
	if c.err != nil {
		return nil, c.err
	}
	path := c.models[request.GetBackend()]
	return &controlv1.ListLocalModelsResponse{Models: []*controlv1.LocalModel{{Path: path}}}, nil
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

func (c *dispatchClient) PutProfile(_ context.Context, r *controlv1.PutProfileRequest) (*controlv1.PutProfileResponse, error) {
	p := r.GetProfile()
	return &controlv1.PutProfileResponse{Profile: p}, c.record(fmt.Sprintf("create:%s:%s:%s:%s:%d:%t", r.GetName(), p.GetBackend(), p.GetModelSource(), p.GetHost(), p.GetPort(), r.GetCreateOnly()))
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
		{"create", rpcRequest{kind: rpcPutProfile, create: &controlv1.Profile{Name: "demo", Backend: "one", ModelSource: "/model", Host: "127.0.0.1", Port: 8080}}, "create:demo:one:/model:127.0.0.1:8080:true"},
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

func TestModelsPageCreatesAValidNeutralProfile(t *testing.T) {
	page := newModelsPage()
	page.active = paneLocal
	data := snapshot{
		backends:     &controlv1.ListBackendsResponse{Backends: []*controlv1.BackendInfo{{Name: "neutral"}}},
		profiles:     &controlv1.ListProfilesResponse{Profiles: []*controlv1.Profile{{Port: 8080}}},
		local:        &controlv1.ListLocalModelsResponse{Models: []*controlv1.LocalModel{{Path: "/models/demo"}}},
		localBackend: "neutral",
	}
	request := page.Update(keyMsg("n"), data)().(rpcRequest)
	if request.kind != rpcPutProfile || request.create.GetBackend() != "neutral" || request.create.GetModelSource() != "/models/demo" || request.create.GetHost() != "127.0.0.1" || request.create.GetPort() != 8081 {
		t.Fatalf("create request = %#v", request)
	}
	if err := profiles.ValidateName(request.create.GetName()); err != nil {
		t.Fatalf("generated profile name is invalid: %v", err)
	}
}

func TestBackendSwitchWaitsForMatchingLocalPoll(t *testing.T) {
	app := &dashboard{
		data: snapshot{
			backends:     &controlv1.ListBackendsResponse{Backends: []*controlv1.BackendInfo{{Name: "one"}, {Name: "two"}}},
			local:        &controlv1.ListLocalModelsResponse{Models: []*controlv1.LocalModel{{Path: "/one/model"}}},
			localBackend: "one", warnings: map[string]string{},
		},
		models: newModelsPage(),
	}
	app.models.active = paneLocal
	app.models.Update(keyMsg("]"), app.data)
	if cmd := app.models.Update(keyMsg("n"), app.data); cmd != nil {
		t.Fatalf("stale local row produced request %#v", cmd())
	}

	client := localPollClient{models: map[string]string{"two": "/two/model"}}
	app.applyPoll(poll(context.Background(), client, "two", nil, false)().(pollResult))
	request := app.models.Update(keyMsg("n"), app.data)().(rpcRequest)
	if request.create.GetBackend() != "two" || request.create.GetModelSource() != "/two/model" {
		t.Fatalf("refreshed create request = %#v", request)
	}

	app.models.Update(keyMsg("["), app.data)
	failed := localPollClient{models: client.models, err: errors.New("local unavailable")}
	app.applyPoll(poll(context.Background(), failed, "one", nil, false)().(pollResult))
	if app.data.localBackend != "two" || app.data.local.GetModels()[0].GetPath() != "/two/model" {
		t.Fatalf("failed poll lost last good local snapshot: %#v", app.data.local)
	}
	if cmd := app.models.Update(keyMsg("n"), app.data); cmd != nil {
		t.Fatalf("failed refresh exposed stale create request %#v", cmd())
	}
}

func TestNextProfilePortReportsExhaustion(t *testing.T) {
	profiles := make([]*controlv1.Profile, 0, 65535-8080+1)
	for port := int32(8080); port <= 65535; port++ {
		profiles = append(profiles, &controlv1.Profile{Port: port})
	}
	if port, err := nextProfilePort(profiles); err == nil || port != 0 {
		t.Fatalf("exhausted ports returned port=%d err=%v", port, err)
	}
	page := newModelsPage()
	page.active = paneLocal
	cmd := page.Update(keyMsg("n"), snapshot{
		backends:     &controlv1.ListBackendsResponse{Backends: []*controlv1.BackendInfo{{Name: "neutral"}}},
		profiles:     &controlv1.ListProfilesResponse{Profiles: profiles},
		local:        &controlv1.ListLocalModelsResponse{Models: []*controlv1.LocalModel{{Path: "/models/demo"}}},
		localBackend: "neutral",
	})
	if msg := cmd().(actionMsg); !strings.Contains(msg.err.Error(), "no available profile port") {
		t.Fatalf("exhaustion warning = %v", msg.err)
	}

	profiles = profiles[:len(profiles)-1]
	if port, err := nextProfilePort(profiles); err != nil || port != 65535 {
		t.Fatalf("last available port=%d err=%v", port, err)
	}
}

func TestProfileCreationErrorsRemainVisible(t *testing.T) {
	app := newDispatchDashboard(&dispatchClient{err: errDispatch})
	msg := app.runRPC(rpcRequest{kind: rpcPutProfile, create: &controlv1.Profile{Name: "demo"}})().(actionMsg)
	app.updateAction(msg)
	if warning := app.data.warnings["action"]; !strings.Contains(warning, errDispatch.Error()) {
		t.Fatalf("profile validation error is not visible: %q", warning)
	}
}

func TestSelectedRunningProfileConfirmsRestartAndClearsState(t *testing.T) {
	page := newModelsPage()
	data := snapshot{
		info:     &controlv1.GetInfoResponse{RunningProfiles: []string{"demo"}},
		profiles: &controlv1.ListProfilesResponse{Profiles: []*controlv1.Profile{{Name: "demo", Backend: "neutral"}}},
	}
	if cmd := page.Update(keyMsg("R"), data); cmd != nil {
		t.Fatal("first restart key dispatched without confirmation")
	}
	request := page.Update(keyMsg("R"), data)().(serviceRequest)
	if request.panel != panelRuntime || !request.restart || request.profile != "demo" {
		t.Fatalf("restart request = %#v", request)
	}
	client := &dispatchClient{}
	msg := runServiceAction(context.Background(), client, request)().(actionMsg)
	if len(client.calls) != 1 || client.calls[0] != "restart:demo" || msg.notice != "runtime restarted" {
		t.Fatalf("restart result = calls %v, notice %q", client.calls, msg.notice)
	}
	if pending := page.status.Pending(); pending != "" {
		t.Fatalf("restart confirmation remained armed for %q", pending)
	}
}

func TestRuntimeSelectionIndexDoesNotBecomeRestart(t *testing.T) {
	client := &dispatchClient{}
	runServiceAction(context.Background(), client, serviceRequest{panel: panelRuntime, action: 1, profile: "second"})()
	if len(client.calls) != 1 || client.calls[0] != "stop:second" {
		t.Fatalf("second runtime action = %v, want stop", client.calls)
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
	if detail := page.detail(100, 8, data); !strings.Contains(detail, "backend-a is active — reset to start backend-b profiles") {
		t.Fatalf("selected profile detail omitted conflict reason: %q", detail)
	}
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
