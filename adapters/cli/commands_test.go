package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

type testClient struct {
	controlv1connect.ControlServiceClient
}

func (testClient) ListBackends(context.Context, *controlv1.ListBackendsRequest) (*controlv1.ListBackendsResponse, error) {
	return &controlv1.ListBackendsResponse{Ok: true, Backends: []*controlv1.BackendInfo{{Name: "test"}}}, nil
}

func TestCommandsUseCanonicalClient(t *testing.T) {
	var output bytes.Buffer
	commands := Commands(func(string, time.Duration) (controlv1connect.ControlServiceClient, error) {
		return testClient{}, nil
	})
	command := commands[0]
	command.SetOut(&output)
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var response controlv1.ListBackendsResponse
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.GetBackends()[0].GetName() != "test" {
		t.Fatalf("response = %#v", &response)
	}
}
