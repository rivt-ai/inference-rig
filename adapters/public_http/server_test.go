package public_http

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

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

func (testClient) StartRuntime(context.Context, *controlv1.StartRuntimeRequest) (*controlv1.StartRuntimeResponse, error) {
	return &controlv1.StartRuntimeResponse{Ok: true}, nil
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

// Every procedure requires the token — reads included, streams included. This
// replaces the old classification table: there is nothing left to add a new RPC
// to, so a new RPC cannot slip through unclassified, and the descriptor is what
// decides the list rather than a hand-maintained set.
func TestEveryProcedureRequiresAuth(t *testing.T) {
	unary, streaming := controlProcedures(t)
	procedures := append(append([]string{}, unary...), streaming...)
	if len(procedures) == 0 {
		t.Fatal("no procedures discovered")
	}
	for _, procedure := range procedures {
		t.Run(procedure, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, procedure, strings.NewReader(`{}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			NewHandler(Dependencies{Control: testClient{}, AuthToken: "secret"}).ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d %q, want 401", response.Code, response.Body.String())
			}
		})
	}
}

// The MCP endpoint is a separate route and therefore a separate guard.
func TestMCPRequiresAuth(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	NewHandler(Dependencies{Control: testClient{}, AuthToken: "secret"}).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

// DisableAuth is the local-only escape hatch: mutations must go through with no
// Authorization header at all, and the MCP endpoint must open with them.
func TestDisableAuthServesMutationsUnauthenticated(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost,
		controlv1connect.ControlServiceStartRuntimeProcedure, strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	NewHandler(Dependencies{Control: testClient{}, DisableAuth: true}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d %q, want 200", response.Code, response.Body.String())
	}

	mcp := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	mcp.Header.Set("Content-Type", "application/json")
	mcpResponse := httptest.NewRecorder()
	NewHandler(Dependencies{Control: testClient{}, DisableAuth: true}).ServeHTTP(mcpResponse, mcp)
	if mcpResponse.Code == http.StatusUnauthorized {
		t.Fatal("/mcp returned 401 with DisableAuth")
	}
}

// The mirror of TestEveryProcedureRequiresAuth: the token has to actually let a
// read through, or "everything is rejected" would pass with a broken gateway.
func TestReadWithTokenSucceeds(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost,
		controlv1connect.ControlServiceListBackendsProcedure, strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	NewHandler(Dependencies{Control: testClient{}, AuthToken: "secret"}).ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"test"`) {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

// An empty configured token used to disable the guard entirely. It must now
// produce a real token instead.
func TestResolveAuthTokenFailsClosed(t *testing.T) {
	token, generated := ResolveAuthToken("", "")
	if !generated || len(token) != 64 {
		t.Fatalf("generated = %v, token length = %d", generated, len(token))
	}
	if again, _ := ResolveAuthToken("", ""); again == token {
		t.Fatal("generated tokens must not repeat")
	}
	if token, generated := ResolveAuthToken("configured", ""); generated || token != "configured" {
		t.Fatalf("configured token = %q, generated = %v", token, generated)
	}
}

// With every read authenticated, a token that did not survive a restart would
// leave the web UI blank after every restart. It must persist and be reused.
func TestResolveAuthTokenPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run", "gateway.token")
	first, generated := ResolveAuthToken("", path)
	if !generated {
		t.Fatal("first call must generate")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("token file: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("token file mode = %04o, want 0600", mode)
	}
	second, generated := ResolveAuthToken("", path)
	if generated || second != first {
		t.Fatalf("reused token = %q (generated %v), want %q", second, generated, first)
	}
	// The environment still wins, so an operator can override the stored token.
	if token, _ := ResolveAuthToken("from-env", path); token != "from-env" {
		t.Fatalf("configured token = %q, want the environment value", token)
	}
}

func TestOriginGuard(t *testing.T) {
	tests := []struct {
		name, origin string
		allowed      []string
		disabled     bool
		want         int
	}{
		{name: "no origin is a non-browser caller", origin: "", want: http.StatusOK},
		{name: "loopback ip", origin: "http://127.0.0.1:7000", want: http.StatusOK},
		{name: "localhost", origin: "http://localhost:7000", want: http.StatusOK},
		{name: "ipv6 loopback", origin: "http://[::1]:7000", want: http.StatusOK},
		{name: "rebinding attempt", origin: "http://evil.example.com", want: http.StatusForbidden},
		{name: "disabled lets anything through", origin: "http://evil.example.com", disabled: true, want: http.StatusOK},
		// A configured list replaces the loopback default entirely, and holds
		// more than one entry: one install is commonly reached by both a
		// hostname and an IP, which a single-origin setting could not express.
		{name: "first configured origin", origin: "https://rig.example",
			allowed: []string{"https://rig.example", "http://10.0.0.4:7000"}, want: http.StatusOK},
		{name: "second configured origin", origin: "http://10.0.0.4:7000",
			allowed: []string{"https://rig.example", "http://10.0.0.4:7000"}, want: http.StatusOK},
		{name: "origin outside the configured list", origin: "http://evil.example.com",
			allowed: []string{"https://rig.example"}, want: http.StatusForbidden},
		{name: "a configured list excludes loopback", origin: "http://127.0.0.1:7000",
			allowed: []string{"https://rig.example"}, want: http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/health", nil)
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			response := httptest.NewRecorder()
			NewHandler(Dependencies{Control: testClient{}, AllowedOrigins: test.allowed, DisableOriginCheck: test.disabled}).
				ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
		})
	}
}

// /health is unauthenticated on purpose — a container healthcheck cannot hold a
// token — and reports the auth posture, which is how a monitor or the web UI
// discovers an open gateway without having watched the startup log.
func TestHealthEndpointIsUnauthenticatedAndReportsPosture(t *testing.T) {
	for _, test := range []struct {
		name        string
		disableAuth bool
		want        string
	}{
		{name: "default", want: `"auth":"required"`},
		{name: "insecure mode", disableAuth: true, want: `"auth":"disabled"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			NewHandler(Dependencies{Control: testClient{}, AuthToken: "secret", DisableAuth: test.disableAuth}).
				ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
			body := response.Body.String()
			if response.Code != http.StatusOK || !strings.Contains(body, `"ok":true`) {
				t.Fatalf("response = %d %q", response.Code, body)
			}
			if !strings.Contains(body, test.want) {
				t.Errorf("body %q does not report %s", body, test.want)
			}
		})
	}
}

// The static app shell stays open: it is what serves the UI that supplies the
// token, and it carries no user data.
func TestAppShellIsUnauthenticated(t *testing.T) {
	shell := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html><script></script></html>")}}
	response := httptest.NewRecorder()
	NewHandler(Dependencies{Control: testClient{}, AuthToken: "secret", AppFS: shell}).
		ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "<html") {
		t.Fatalf("GET / = %d %q", response.Code, response.Body.String())
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
