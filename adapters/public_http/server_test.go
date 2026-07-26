package public_http

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

func TestRESTFacadeUsesCanonicalClient(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/backends", nil)
	response := httptest.NewRecorder()
	NewHandler(Dependencies{Control: testClient{}}).ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"test"`) {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}
