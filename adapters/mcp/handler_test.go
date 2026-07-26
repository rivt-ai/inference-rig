package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

type testClient struct {
	controlv1connect.ControlServiceClient
}

func (testClient) ListBackends(context.Context, *controlv1.ListBackendsRequest) (*controlv1.ListBackendsResponse, error) {
	return &controlv1.ListBackendsResponse{Ok: true, Backends: []*controlv1.BackendInfo{{Name: "test"}}}, nil
}

func TestToolsCallCanonicalClient(t *testing.T) {
	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"backends_list","arguments":{}}}`)
	request := httptest.NewRequest(http.MethodPost, "/mcp", body)
	response := httptest.NewRecorder()
	NewHandler(testClient{}).ServeHTTP(response, request)
	if !strings.Contains(response.Body.String(), `\"name\":\"test\"`) {
		t.Fatalf("response = %q", response.Body.String())
	}
}
