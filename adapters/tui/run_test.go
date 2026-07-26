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
