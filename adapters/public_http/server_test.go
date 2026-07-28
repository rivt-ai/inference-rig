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

func (testClient) Health(context.Context, *controlv1.HealthRequest) (*controlv1.HealthResponse, error) {
	return &controlv1.HealthResponse{Ok: true, Service: "inferencerig"}, nil
}

func (testClient) ListBackends(context.Context, *controlv1.ListBackendsRequest) (*controlv1.ListBackendsResponse, error) {
	return &controlv1.ListBackendsResponse{Ok: true, Backends: []*controlv1.BackendInfo{{Name: "test"}}}, nil
}

// controlProcedures returns every procedure on the service, split by whether it
// is a server stream, straight from the descriptor so the tests below cannot
// drift as RPCs are added.
func controlProcedures(t *testing.T) (unary, streaming []string) {
	t.Helper()
	methods := controlv1.File_inferencerig_control_v1_control_proto.
		Services().ByName("ControlService").Methods()
	for i := range methods.Len() {
		method := methods.Get(i)
		procedure := "/inferencerig.control.v1.ControlService/" + string(method.Name())
		if method.IsStreamingServer() {
			streaming = append(streaming, procedure)
			continue
		}
		unary = append(unary, procedure)
	}
	return unary, streaming
}

// The gateway must re-export the canonical service unchanged: a call to a
// procedure has to arrive upstream as that same procedure. This replaces the
// old table of 28 hand-written REST routes, which could only ever test the
// routes someone remembered to add.
func TestGatewayForwardsEveryProcedureUnchanged(t *testing.T) {
	unary, _ := controlProcedures(t)
	if len(unary) == 0 {
		t.Fatal("no unary procedures discovered")
	}
	for _, procedure := range unary {
		t.Run(procedure, func(t *testing.T) {
			recorder := &routeRecorder{}
			client := controlv1connect.NewControlServiceClient(&http.Client{Transport: recorder}, "http://control")
			request := httptest.NewRequest(http.MethodPost, procedure, strings.NewReader(`{}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer secret")
			NewHandler(Dependencies{Control: client, AuthToken: "secret"}).
				ServeHTTP(httptest.NewRecorder(), request)
			recorder.mu.Lock()
			called := recorder.path
			recorder.mu.Unlock()
			if called != procedure {
				t.Fatalf("called %q, want %q", called, procedure)
			}
		})
	}
}

// Every procedure named with a mutating verb must be in mutatingProcedures.
// Without this, adding a write RPC silently ships it unauthenticated.
func TestEveryMutatingProcedureRequiresAuth(t *testing.T) {
	mutatingVerbs := []string{"Put", "Delete", "Set", "Install", "Start", "Stop", "Restart", "Cancel", "Apply", "Cleanup", "Clear"}
	unary, _ := controlProcedures(t)
	for _, procedure := range unary {
		name := procedure[strings.LastIndex(procedure, "/")+1:]
		mutating := false
		for _, verb := range mutatingVerbs {
			if strings.HasPrefix(name, verb) {
				mutating = true
				break
			}
		}
		if !mutating {
			continue
		}
		if _, guarded := mutatingProcedures[procedure]; !guarded {
			t.Errorf("%s looks mutating but is not in mutatingProcedures", name)
		}
	}
}

// Guard against the reverse drift: a stale entry for a procedure that no longer
// exists silently protects nothing.
func TestMutatingProceduresAllExist(t *testing.T) {
	unary, _ := controlProcedures(t)
	known := make(map[string]struct{}, len(unary))
	for _, procedure := range unary {
		known[procedure] = struct{}{}
	}
	for procedure := range mutatingProcedures {
		if _, ok := known[procedure]; !ok {
			t.Errorf("mutatingProcedures lists unknown procedure %q", procedure)
		}
	}
}

func TestMutationWithoutTokenIsRejected(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost,
		controlv1connect.ControlServiceStartRuntimeProcedure, strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	NewHandler(Dependencies{Control: testClient{}, AuthToken: "secret"}).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestReadsAreOpen(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost,
		controlv1connect.ControlServiceListBackendsProcedure, strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	NewHandler(Dependencies{Control: testClient{}, AuthToken: "secret"}).ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"test"`) {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

// An empty configured token used to disable the guard entirely. It must now
// produce a real token instead.
func TestResolveAuthTokenFailsClosed(t *testing.T) {
	token, generated := ResolveAuthToken("")
	if !generated || len(token) != 64 {
		t.Fatalf("generated = %v, token length = %d", generated, len(token))
	}
	if again, _ := ResolveAuthToken(""); again == token {
		t.Fatal("generated tokens must not repeat")
	}
	if token, generated := ResolveAuthToken("configured"); generated || token != "configured" {
		t.Fatalf("configured token = %q, generated = %v", token, generated)
	}
}

func TestOriginGuard(t *testing.T) {
	tests := []struct {
		name, origin string
		disabled     bool
		want         int
	}{
		{name: "no origin is a non-browser caller", origin: "", want: http.StatusOK},
		{name: "loopback ip", origin: "http://127.0.0.1:7000", want: http.StatusOK},
		{name: "localhost", origin: "http://localhost:7000", want: http.StatusOK},
		{name: "ipv6 loopback", origin: "http://[::1]:7000", want: http.StatusOK},
		{name: "rebinding attempt", origin: "http://evil.example.com", want: http.StatusForbidden},
		{name: "disabled lets anything through", origin: "http://evil.example.com", disabled: true, want: http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/health", nil)
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			response := httptest.NewRecorder()
			NewHandler(Dependencies{Control: testClient{}, DisableOriginCheck: test.disabled}).
				ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
		})
	}
}

func TestHealthEndpointStaysPlainHTTP(t *testing.T) {
	response := httptest.NewRecorder()
	NewHandler(Dependencies{Control: testClient{}}).
		ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"ok":true`) {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

// The REST facade is gone; nothing may answer under /api/ any more.
func TestRESTFacadeIsRemoved(t *testing.T) {
	for _, target := range []string{"/api/backends", "/api/profiles", "/api/info", "/api/signals"} {
		response := httptest.NewRecorder()
		NewHandler(Dependencies{Control: testClient{}}).
			ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", target, response.Code)
		}
	}
}
