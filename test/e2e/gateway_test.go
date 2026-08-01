//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

// TestPublicGateway drives the browser-facing surface of a running install:
// the same compiled `inferencerig web` process, the same Connect handler, the
// same MCP endpoint, and the same auth and origin guards a browser meets.
//
// The Go Connect client is used deliberately rather than hand-rolled HTTP: it
// speaks the wire protocol the web app speaks, so a change that breaks the
// browser breaks this test too.
func TestPublicGateway(t *testing.T) {
	rig := newLlamacppRig(t)
	rig.startControl()
	gateway := rig.startGateway()
	base := rig.gatewayURL()

	// 1. The plain health route, which container healthchecks and shell scripts
	// use because they cannot speak Connect.
	if status, body := httpGet(t, base+"/health", nil); status != http.StatusOK || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("GET /health = %d %s", status, body)
	}

	anonymous := controlv1connect.NewControlServiceClient(http.DefaultClient, base)
	authorized := controlv1connect.NewControlServiceClient(http.DefaultClient, base,
		connect.WithInterceptors(bearer(rig.token)))
	ctx := context.Background()

	// 2. Every procedure is refused without the token — the read as much as the
	// write. A read exposes profiles, installed models, runtime state,
	// telemetry, logs and audit records, which is the whole state of the
	// machine, so "reads are open" is not a smaller hole than an open write.
	_, err := anonymous.PutProfile(ctx, &controlv1.PutProfileRequest{
		Name: "unauthorized", ProfileYaml: rig.profileYAML("unauthorized", 9999), CreateOnly: true,
	})
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("unauthenticated PutProfile = %v (want unauthenticated)", err)
	}
	if _, err := anonymous.ListBackends(ctx, &controlv1.ListBackendsRequest{}); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("unauthenticated ListBackends = %v (want unauthenticated)", err)
	}
	// Streams are a separate code path in connect-go: a unary-only guard leaves
	// the machine's whole event feed readable.
	stream, err := anonymous.WatchEvents(ctx, &controlv1.WatchEventsRequest{})
	if err == nil {
		stream.Receive()
		err = stream.Err()
		_ = stream.Close()
	}
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("unauthenticated WatchEvents = %v (want unauthenticated)", err)
	}

	// The unauthenticated exceptions, both deliberate: /health so a container
	// healthcheck can reach it, and / so the shell that supplies the token can
	// load. /health also publishes the posture, which is how the QA script and
	// any monitor tell an open gateway from a protected one.
	if _, body := httpGet(t, base+"/health", nil); !strings.Contains(body, `"auth":"required"`) {
		t.Errorf("GET /health did not report the auth posture: %s", body)
	}

	// 3. MCP is a separate protocol on its own route, so its guard is separate
	// code and needs its own proof.
	status, _ := rig.mcp(t, "", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	if status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /mcp = %d (want 401)", status)
	}

	// 4. The same calls succeed with the token.
	created, err := authorized.PutProfile(ctx, &controlv1.PutProfileRequest{
		Name: "gateway", ProfileYaml: rig.profileYAML("gateway", freePort(t)), CreateOnly: true,
	})
	if err != nil || !created.GetOk() {
		t.Fatalf("authenticated PutProfile = %v, err = %v", created, err)
	}

	// 5. A browser origin that is not approved is refused before any handler
	// runs; this is the DNS-rebinding guard on a loopback gateway holding a
	// token the browser would otherwise spend automatically.
	if status, _ := httpGet(t, base+"/health", map[string]string{"Origin": "http://evil.example"}); status != http.StatusForbidden {
		t.Errorf("cross-origin GET /health = %d (want 403)", status)
	}
	if status, _ := httpGet(t, base+"/health", map[string]string{"Origin": base}); status != http.StatusOK {
		t.Errorf("same-origin GET /health = %d (want 200)", status)
	}

	// 6. MCP discovery plus one read-only tool call, end to end over the socket.
	rig.assertMCPTools(t)

	// 7. The built application shell is what the browser actually loads.
	assertAppShell(t, base)

	// 8. The public stream bridge: a real server stream forwarded from the
	// control socket through the gateway, then cancelled by the client. This is
	// the path a UI panel opens and closes on every navigation.
	assertStreamCancels(t, rig, authorized)

	// 9. Stopping the gateway must clear the PID file it self-registered, or the
	// TUI's "Stop" will keep offering to stop a process that is already gone.
	gateway.stop(t)
	waitFor(t, "gateway PID cleanup", func() bool { return !fileExists(rig.pidPath("web")) })
}

// TestGatewayBehindReverseProxy asserts the configuration documented in
// docs/reverse-proxy.md: with security.allowed_origins set, the browser origin
// the proxy forwards is accepted and everything else is still refused. The unit
// tests cover the matching itself; what this proves is the wiring — that the
// configured list actually reaches the origin guard in a real process, which is
// what the documented deployment depends on.
func TestGatewayBehindReverseProxy(t *testing.T) {
	rig := newLlamacppRig(t)
	rig.writeConfig("security:\n  allowed_origins:\n    - \"https://rig.example\"\n")
	rig.startControl()
	gateway := rig.startGateway()
	defer gateway.stop(t)

	for _, test := range []struct {
		origin string
		want   int
	}{
		{origin: "https://rig.example", want: http.StatusOK},
		{origin: "https://evil.example", want: http.StatusForbidden},
		// A configured list replaces the loopback default rather than adding to
		// it, so a proxied install stops trusting loopback origins as well.
		{origin: rig.gatewayURL(), want: http.StatusForbidden},
	} {
		if status, _ := httpGet(t, rig.gatewayURL()+"/health", map[string]string{"Origin": test.origin}); status != test.want {
			t.Errorf("Origin %s = %d, want %d", test.origin, status, test.want)
		}
	}
}

// TestGatewayInsecureMode proves the other half of the policy: the escape hatch
// still works, and someone running it cannot come to believe they are
// protected. The posture must be discoverable by someone who never watched the
// terminal — a systemd-managed daemon has no terminal — so /health carries it
// as well as the startup message.
//
// It gets its own install rather than restarting the gateway inside
// TestPublicGateway: the control daemon owns config.yaml and rewrites it whole
// from its own copy (an autostart toggle is enough), so a security block edited
// in underneath a running daemon does not survive to be read.
func TestGatewayInsecureMode(t *testing.T) {
	rig := newLlamacppRig(t)
	rig.writeConfig("security:\n  disable_auth: true\n")
	rig.startControl()
	gateway := rig.startGateway()
	defer gateway.stop(t)

	if _, body := httpGet(t, rig.gatewayURL()+"/health", nil); !strings.Contains(body, `"auth":"disabled"`) {
		t.Errorf("insecure /health did not report the posture: %s", body)
	}
	anonymous := controlv1connect.NewControlServiceClient(http.DefaultClient, rig.gatewayURL())
	if _, err := anonymous.ListBackends(context.Background(), &controlv1.ListBackendsRequest{}); err != nil {
		t.Fatalf("insecure mode rejected an unauthenticated read: %v", err)
	}
	// The startup message must name the setting and what it exposes, not merely
	// mention that something is different.
	output := gateway.output()
	for _, want := range []string{"disable_auth", "without authentication"} {
		if !strings.Contains(output, want) {
			t.Errorf("insecure startup output does not mention %q:\n%s", want, output)
		}
	}
}

// bearer attaches the gateway token the way the web app does — on streams as
// well as unary calls. connect.UnaryInterceptorFunc would cover only the unary
// half, which is the same gap that made a unary-only server guard leave
// WatchEvents open.
type bearer string

func (b bearer) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
		request.Header().Set("Authorization", "Bearer "+string(b))
		return next(ctx, request)
	}
}

func (b bearer) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		conn := next(ctx, spec)
		conn.RequestHeader().Set("Authorization", "Bearer "+string(b))
		return conn
	}
}

func (bearer) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

// mcp posts one JSON-RPC message to the MCP route, optionally authenticated.
func (r *rig) mcp(t *testing.T, token string, message map[string]any) (int, map[string]any) {
	t.Helper()
	body, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, r.gatewayURL()+"/mcp", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	var decoded map[string]any
	_ = json.NewDecoder(response.Body).Decode(&decoded)
	return response.StatusCode, decoded
}

func (r *rig) assertMCPTools(t *testing.T) {
	t.Helper()
	status, listed := r.mcp(t, r.token, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	if status != http.StatusOK {
		t.Fatalf("tools/list = %d", status)
	}
	result, _ := listed["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	if len(tools) == 0 {
		t.Fatalf("tools/list returned no tools: %v", listed)
	}
	status, called := r.mcp(t, r.token, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": "backends_list", "arguments": map[string]any{}},
	})
	if status != http.StatusOK || called["error"] != nil {
		t.Fatalf("tools/call backends_list = %d %v", status, called)
	}
	if !strings.Contains(mustJSON(t, called), "llamacpp") {
		t.Errorf("backends_list did not reach the real registry: %v", called)
	}
}

// assertStreamCancels opens a forwarded server stream and cancels it, proving
// both that the bridge forwards and that a cancelled browser request releases
// the control-socket connection instead of leaking it.
func assertStreamCancels(t *testing.T, rig *rig, client controlv1connect.ControlServiceClient) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := client.WatchEvents(ctx, &controlv1.WatchEventsRequest{})
	if err != nil {
		t.Fatalf("WatchEvents: %v", err)
	}
	// Any audited operation produces an event on the open stream.
	go func() {
		_, _ = client.SetProfileAutostart(context.Background(),
			&controlv1.SetProfileAutostartRequest{Name: "gateway", Enabled: true})
	}()
	if !stream.Receive() {
		t.Fatalf("stream produced no events: %v", stream.Err())
	}
	cancel()
	_ = stream.Close()
	// The gateway must stay healthy after the cancellation; a bridge that
	// deadlocked on a cancelled downstream would fail here.
	if status, _ := httpGet(t, rig.gatewayURL()+"/health", nil); status != http.StatusOK {
		t.Errorf("gateway unhealthy after stream cancellation: %d", status)
	}
}

// assertAppShell checks the gateway serves the built Svelte application. The
// built app is a hard requirement rather than a conditional assertion: a
// gateway that serves no UI is a broken gateway, and `make e2e` builds it.
func assertAppShell(t *testing.T, base string) {
	t.Helper()
	status, body := httpGet(t, base+"/", nil)
	if status != http.StatusOK {
		t.Fatalf("GET / = %d (run `make webui` to build the app the gateway embeds)", status)
	}
	if !strings.Contains(body, "<html") || !strings.Contains(body, "<script") {
		t.Errorf("GET / did not serve the application shell: %.200s", body)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
