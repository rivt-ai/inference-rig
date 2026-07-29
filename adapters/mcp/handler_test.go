package mcp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

type routeRecorder struct {
	mu   sync.Mutex
	path string
}

func (r *routeRecorder) RoundTrip(request *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.path = request.URL.Path
	r.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusNotImplemented, Status: "501 Not Implemented",
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(strings.NewReader(`{"code":"unimplemented"}`)),
	}, nil
}

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

func TestToolBreadthRoutesOnlyToCanonicalRPC(t *testing.T) {
	tests := []struct {
		name, arguments, procedure string
	}{
		{"backends_list", `{}`, controlv1connect.ControlServiceListBackendsProcedure},
		{"backend_install_status", `{"backend":"test"}`, controlv1connect.ControlServiceGetBackendInstallStatusProcedure},
		{"backend_install", `{"backend":"test"}`, controlv1connect.ControlServiceInstallBackendProcedure},
		{"backend_params", `{"backend":"test"}`, controlv1connect.ControlServiceGetBackendParamsProcedure},
		{"profiles_list", `{}`, controlv1connect.ControlServiceListProfilesProcedure},
		{"profile_get", `{"name":"demo"}`, controlv1connect.ControlServiceGetProfileProcedure},
		{"profile_put", `{"name":"demo","profile_yaml":"version: 1"}`, controlv1connect.ControlServicePutProfileProcedure},
		{"profile_delete", `{"name":"demo"}`, controlv1connect.ControlServiceDeleteProfileProcedure},
		{"profile_cleanup", `{"name":"demo"}`, controlv1connect.ControlServiceCleanupProfileProcedure},
		{"profile_autostart", `{"name":"demo","enabled":true}`, controlv1connect.ControlServiceSetProfileAutostartProcedure},
		{"catalog_search", `{"backend":"test"}`, controlv1connect.ControlServiceListModelCatalogProcedure},
		{"models_local", `{"backend":"test"}`, controlv1connect.ControlServiceListLocalModelsProcedure},
		{"model_delete", `{"backend":"test","path":"x"}`, controlv1connect.ControlServiceDeleteLocalModelProcedure},
		{"model_resolve", `{"profile":"demo"}`, controlv1connect.ControlServiceResolveProfileModelProcedure},
		{"download_start", `{"profile":"demo"}`, controlv1connect.ControlServiceStartModelDownloadProcedure},
		{"download_get", `{"id":"dl"}`, controlv1connect.ControlServiceGetModelDownloadProcedure},
		{"download_cancel", `{"id":"dl"}`, controlv1connect.ControlServiceCancelModelDownloadProcedure},
		{"download_apply", `{"profile":"demo","id":"dl"}`, controlv1connect.ControlServiceApplyDownloadToProfileProcedure},
		{"runtime_status", `{"profile":"demo"}`, controlv1connect.ControlServiceGetRuntimeStatusProcedure},
		{"runtime_start", `{"profile":"demo"}`, controlv1connect.ControlServiceStartRuntimeProcedure},
		{"runtime_stop", `{"profile":"demo"}`, controlv1connect.ControlServiceStopRuntimeProcedure},
		{"runtime_restart", `{"profile":"demo"}`, controlv1connect.ControlServiceRestartRuntimeProcedure},
		{"info_get", `{}`, controlv1connect.ControlServiceGetInfoProcedure},
		{"signals_get", `{}`, controlv1connect.ControlServiceGetSignalsProcedure},
		{"events_list", `{}`, controlv1connect.ControlServiceListEventsProcedure},
		{"startup_services_set", `{"services":["control"]}`, controlv1connect.ControlServiceSetStartupServicesProcedure},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := &routeRecorder{}
			client := controlv1connect.NewControlServiceClient(&http.Client{Transport: recorder}, "http://control")
			body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, test.name, test.arguments)
			request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
			NewHandler(client).ServeHTTP(httptest.NewRecorder(), request)
			recorder.mu.Lock()
			called := recorder.path
			recorder.mu.Unlock()
			if called != test.procedure {
				t.Fatalf("called %q, want %q", called, test.procedure)
			}
		})
	}
}
