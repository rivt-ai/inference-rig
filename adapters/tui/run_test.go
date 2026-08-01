package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

type testClient struct {
	controlv1connect.ControlServiceClient
}

func (testClient) ListBackends(context.Context, *controlv1.ListBackendsRequest) (*controlv1.ListBackendsResponse, error) {
	return &controlv1.ListBackendsResponse{Backends: []*controlv1.BackendInfo{{Name: "one"}, {Name: "two"}}}, nil
}
func (testClient) ListProfiles(context.Context, *controlv1.ListProfilesRequest) (*controlv1.ListProfilesResponse, error) {
	return &controlv1.ListProfilesResponse{Profiles: []*controlv1.Profile{{Name: "demo", Backend: "one", ModelReference: "owner/model"}}}, nil
}
func (testClient) GetInfo(context.Context, *controlv1.GetInfoRequest) (*controlv1.GetInfoResponse, error) {
	return &controlv1.GetInfoResponse{Backends: 2, Profiles: 1, RunningProfiles: []string{"demo"}}, nil
}
func (testClient) GetRuntimeStatus(context.Context, *controlv1.GetRuntimeStatusRequest) (*controlv1.GetRuntimeStatusResponse, error) {
	return &controlv1.GetRuntimeStatusResponse{Profiles: []*controlv1.ProfileRuntimeStatus{{Name: "demo", Status: &controlv1.RuntimeStatus{State: "running"}}}}, nil
}
func (testClient) GetSignals(context.Context, *controlv1.GetSignalsRequest) (*controlv1.GetSignalsResponse, error) {
	return &controlv1.GetSignalsResponse{Signals: &controlv1.Signals{TotalMemoryBytes: 16, AvailableMemoryBytes: 8, CpuUsedPercent: 25}}, nil
}
func (testClient) ListModelCatalog(context.Context, *controlv1.ListModelCatalogRequest) (*controlv1.ListModelCatalogResponse, error) {
	return &controlv1.ListModelCatalogResponse{Models: []*controlv1.CatalogModel{{Id: "owner/model"}}}, nil
}
func (testClient) ListLocalModels(context.Context, *controlv1.ListLocalModelsRequest) (*controlv1.ListLocalModelsResponse, error) {
	return &controlv1.ListLocalModelsResponse{Models: []*controlv1.LocalModel{{Path: "/models/model", Filename: "model"}}}, nil
}
func (testClient) ListEvents(context.Context, *controlv1.ListEventsRequest) (*controlv1.ListEventsResponse, error) {
	return &controlv1.ListEventsResponse{Events: []*controlv1.Event{{Action: "runtime.start", Success: true}}}, nil
}

func TestFrameHasAdaptedPages(t *testing.T) {
	m := newModel(context.Background(), testClient{}, false)
	want := []string{"Services", "Models", "System", "Activity"}
	for i, title := range want {
		active := m.frame.ActivePage()
		if active != 0 {
			t.Fatalf("initial page = %d", active)
		}
		m.app.active = i
		view := (&page{app: m.app, index: i, title: title}).View(120, 24)
		if view == "" {
			t.Fatalf("%s view is empty", title)
		}
	}
}

func TestPollLoadsCanonicalViews(t *testing.T) {
	result := poll(context.Background(), testClient{}, "one", nil, false)().(pollResult)
	// The catalog is deliberately absent: a cold remote catalog outlasts the
	// poll timeout, so it is fetched on its own command instead.
	for _, key := range []string{"base", "local", "signals", "events", "downloads"} {
		if !result.ok[key] {
			t.Fatalf("%s failed: %v", key, result.value.warnings)
		}
	}
	if result.value.profiles.GetProfiles()[0].GetName() != "demo" {
		t.Fatalf("profiles = %#v", result.value.profiles)
	}
	if result.value.catalog != nil {
		t.Fatalf("poll carried a catalog: %#v", result.value.catalog)
	}
}

// The slow catalog command is what fills the table, and one failure must leave
// the last good catalog in place rather than blanking it.
func TestCatalogCommandLoadsAndReportsFailure(t *testing.T) {
	msg := fetchCatalog(context.Background(), testClient{}, "one")().(catalogMsg)
	if msg.err != nil || len(msg.value.GetModels()) == 0 {
		t.Fatalf("catalog = %#v, err = %v", msg.value, msg.err)
	}
	app := &dashboard{data: snapshot{warnings: map[string]string{}}, catalogInFlight: true}
	app.applyCatalog(msg)
	if app.catalogInFlight || app.data.catalogPending || len(app.data.catalog.GetModels()) == 0 {
		t.Fatalf("dashboard = %#v", app.data)
	}
	app.catalogInFlight = true
	app.applyCatalog(catalogMsg{err: errCatalogTest})
	if app.data.warnings["catalog"] == "" || len(app.data.catalog.GetModels()) == 0 {
		t.Fatalf("failed catalog dropped state: %#v", app.data)
	}
}

var errCatalogTest = errors.New("catalog unavailable")

func TestModelsSelectBackendAndConfirmDelete(t *testing.T) {
	page := newModelsPage()
	data := snapshot{
		backends: &controlv1.ListBackendsResponse{Backends: []*controlv1.BackendInfo{{Name: "one"}, {Name: "two"}}},
		local:    &controlv1.ListLocalModelsResponse{Models: []*controlv1.LocalModel{{Path: "/model", Filename: "model"}}},
	}
	page.active = paneLocal
	page.Update(keyMsg("]"), data)
	if got := page.backend(data.backends); got != "two" {
		t.Fatalf("backend = %q", got)
	}
	if cmd := page.Update(keyMsg("d"), data); cmd != nil || page.localStatus.Pending() != "/model" {
		t.Fatalf("first delete cmd=%v pending=%q", cmd, page.localStatus.Pending())
	}
	request := page.Update(keyMsg("y"), data)().(rpcRequest)
	if request.kind != rpcDelete || request.backend != "two" || request.path != "/model" {
		t.Fatalf("request = %#v", request)
	}
}

func TestActivityCapturesSearchInput(t *testing.T) {
	page := newActivityPage()
	page.Update(keyMsg("/"))
	if !page.CapturingInput() {
		t.Fatal("activity did not capture search input")
	}
	page.Update(keyMsg("1"))
	if query := page.views[0].Query(); query != "1" {
		t.Fatalf("query = %q", query)
	}
}

func TestSystemViewRendersMeters(t *testing.T) {
	page := systemPage{}
	view := page.View(100, 20, snapshot{
		info: &controlv1.GetInfoResponse{},
		signals: &controlv1.GetSignalsResponse{Signals: &controlv1.Signals{
			TotalMemoryBytes: 100, AvailableMemoryBytes: 25, CpuUsedPercent: 20,
		}},
	})
	for _, want := range []string{"CPU:", "RAM:", "75%"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q:\n%s", want, view)
		}
	}
}

func keyMsg(value string) tea.KeyPressMsg {
	runes := []rune(value)
	return tea.KeyPressMsg(tea.Key{Text: value, Code: runes[0]})
}
