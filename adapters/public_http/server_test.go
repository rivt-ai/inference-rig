package public_http

import (
	"context"
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

func TestRESTFacadeUsesCanonicalClient(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/backends", nil)
	response := httptest.NewRecorder()
	NewHandler(Dependencies{Control: testClient{}}).ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"test"`) {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

func TestRESTBreadthRoutesOnlyToCanonicalRPC(t *testing.T) {
	tests := []struct {
		method, target, body, procedure string
	}{
		{"GET", "/api/info", "", controlv1connect.ControlServiceGetInfoProcedure},
		{"GET", "/api/profiles/demo", "", controlv1connect.ControlServiceGetProfileProcedure},
		{"PUT", "/api/profiles/demo", `{}`, controlv1connect.ControlServicePutProfileProcedure},
		{"DELETE", "/api/profiles/demo", "", controlv1connect.ControlServiceDeleteProfileProcedure},
		{"POST", "/api/profiles/demo/cleanup", "", controlv1connect.ControlServiceCleanupProfileProcedure},
		{"POST", "/api/profiles/demo/autostart", `{}`, controlv1connect.ControlServiceSetProfileAutostartProcedure},
		{"GET", "/api/catalog?backend=test", "", controlv1connect.ControlServiceListModelCatalogProcedure},
		{"GET", "/api/models/local?backend=test", "", controlv1connect.ControlServiceListLocalModelsProcedure},
		{"DELETE", "/api/models/local?backend=test&path=x", "", controlv1connect.ControlServiceDeleteLocalModelProcedure},
		{"GET", "/api/models/resolve/demo", "", controlv1connect.ControlServiceResolveProfileModelProcedure},
		{"POST", "/api/downloads/demo", "", controlv1connect.ControlServiceStartModelDownloadProcedure},
		{"GET", "/api/downloads/dl", "", controlv1connect.ControlServiceGetModelDownloadProcedure},
		{"POST", "/api/downloads/dl/cancel", "", controlv1connect.ControlServiceCancelModelDownloadProcedure},
		{"POST", "/api/downloads/dl/apply/demo", "", controlv1connect.ControlServiceApplyDownloadToProfileProcedure},
		{"POST", "/api/backends/test/install", `{}`, controlv1connect.ControlServiceInstallBackendProcedure},
		{"GET", "/api/backends/test/install", "", controlv1connect.ControlServiceGetBackendInstallStatusProcedure},
		{"GET", "/api/backends/test/params", "", controlv1connect.ControlServiceGetBackendParamsProcedure},
		{"POST", "/api/runtime/demo/restart", "", controlv1connect.ControlServiceRestartRuntimeProcedure},
		{"GET", "/api/signals", "", controlv1connect.ControlServiceGetSignalsProcedure},
		{"GET", "/api/events", "", controlv1connect.ControlServiceListEventsProcedure},
		{"PUT", "/api/config/startup", `{}`, controlv1connect.ControlServiceSetStartupServicesProcedure},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.target, func(t *testing.T) {
			recorder := &routeRecorder{}
			client := controlv1connect.NewControlServiceClient(&http.Client{Transport: recorder}, "http://control")
			request := httptest.NewRequest(test.method, test.target, strings.NewReader(test.body))
			NewHandler(Dependencies{Control: client}).ServeHTTP(httptest.NewRecorder(), request)
			recorder.mu.Lock()
			called := recorder.path
			recorder.mu.Unlock()
			if called != test.procedure {
				t.Fatalf("called %q, want %q", called, test.procedure)
			}
		})
	}
}

func TestRESTMutationsRequireConfiguredBearerToken(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/demo/start", nil)
	response := httptest.NewRecorder()
	NewHandler(Dependencies{Control: testClient{}, AuthToken: "secret"}).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}
