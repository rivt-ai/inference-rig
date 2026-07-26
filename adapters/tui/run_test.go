package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

type testClient struct {
	controlv1connect.ControlServiceClient
}

func (testClient) ListBackends(context.Context, *controlv1.ListBackendsRequest) (*controlv1.ListBackendsResponse, error) {
	return &controlv1.ListBackendsResponse{Backends: []*controlv1.BackendInfo{{Name: "test"}}}, nil
}

func (testClient) ListProfiles(context.Context, *controlv1.ListProfilesRequest) (*controlv1.ListProfilesResponse, error) {
	return &controlv1.ListProfilesResponse{Profiles: []*controlv1.Profile{{Name: "demo"}}}, nil
}

func (testClient) GetRuntimeStatus(context.Context, *controlv1.GetRuntimeStatusRequest) (*controlv1.GetRuntimeStatusResponse, error) {
	return &controlv1.GetRuntimeStatusResponse{Status: &controlv1.RuntimeStatus{State: "running"}}, nil
}

func (testClient) GetInfo(context.Context, *controlv1.GetInfoRequest) (*controlv1.GetInfoResponse, error) {
	return &controlv1.GetInfoResponse{Backends: 1, Profiles: 1}, nil
}

func (testClient) GetSignals(context.Context, *controlv1.GetSignalsRequest) (*controlv1.GetSignalsResponse, error) {
	return &controlv1.GetSignalsResponse{Signals: &controlv1.Signals{AvailableMemoryBytes: 8}}, nil
}

func (testClient) ListModelCatalog(context.Context, *controlv1.ListModelCatalogRequest) (*controlv1.ListModelCatalogResponse, error) {
	return &controlv1.ListModelCatalogResponse{Models: []*controlv1.CatalogModel{{Id: "owner/model"}}}, nil
}

func (testClient) ListLocalModels(context.Context, *controlv1.ListLocalModelsRequest) (*controlv1.ListLocalModelsResponse, error) {
	return &controlv1.ListLocalModelsResponse{Models: []*controlv1.LocalModel{{Path: "/models/model"}}}, nil
}

func (testClient) ListEvents(context.Context, *controlv1.ListEventsRequest) (*controlv1.ListEventsResponse, error) {
	return &controlv1.ListEventsResponse{Events: []*controlv1.Event{{Action: "runtime.start", Success: true}}}, nil
}

func TestRunRendersCanonicalSnapshots(t *testing.T) {
	var output bytes.Buffer
	if err := Run(context.Background(), &output, testClient{}, "demo"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Backends: 1", "Profiles: 1", "Runtime demo: running"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q", output.String())
		}
	}
}

func TestDashboardLoadsEveryViewThroughCanonicalClient(t *testing.T) {
	for page := range pageCount {
		snapshot, err := loadSnapshot(context.Background(), testClient{}, page, 0, nil)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		model := newModel(context.Background(), testClient{})
		model.page, model.data = page, snapshot
		view := model.View().Content
		if !strings.Contains(view, "InferenceRig") {
			t.Fatalf("page %d view = %q", page, view)
		}
	}
}
