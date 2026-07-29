// Package public_http serves the browser-facing surface: the canonical control
// RPC over Connect, a plain health endpoint, the MCP JSON-RPC endpoint, and the
// embedded web app.
//
// There is deliberately no hand-written REST facade. Every operation the UI
// performs is a ControlService method, so the wire contract is the proto and
// nothing has to be kept in sync by hand.
package public_http

import (
	"io/fs"
	"net/http"

	adaptermcp "inferencerig/adapters/mcp"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

// Dependencies configures the public gateway.
type Dependencies struct {
	// Control is the canonical control client, dialed over the control socket.
	Control controlv1connect.ControlServiceClient
	// AuthToken guards every mutating procedure. Resolve it with
	// ResolveAuthToken so an unset token fails closed rather than opening the
	// gateway.
	AuthToken string
	// DisableAuth serves every procedure unauthenticated. It exists for a
	// single-user local install bound to loopback; the caller is responsible
	// for refusing it on a bind that reaches the network.
	DisableAuth bool
	// AppFS holds the built web app. A nil AppFS serves no static files.
	AppFS fs.FS
	// AllowedOrigin, when set, is the only browser origin permitted to reach
	// the gateway. Empty means loopback-only, which is the default posture.
	AllowedOrigin string
	// DisableOriginCheck turns the origin guard off. It exists for reverse-proxy
	// deployments that terminate the browser origin themselves.
	DisableOriginCheck bool
}

// NewHandler returns the public gateway handler.
func NewHandler(deps Dependencies) http.Handler {
	if deps.Control == nil {
		panic("public_http: control client is required")
	}
	mux := http.NewServeMux()

	// The canonical RPC. Unary methods forward straight through; the two
	// server streams are piped by controlBridge.
	path, handler := controlv1connect.NewControlServiceHandler(
		controlBridge{ControlServiceClient: deps.Control},
		connectInterceptors(deps.AuthToken, deps.DisableAuth),
	)
	mux.Handle(path, handler)

	// A plain health endpoint for load balancers, container healthchecks, and
	// shell scripts, which cannot speak Connect.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		response, err := deps.Control.Health(r.Context(), &controlv1.HealthRequest{})
		if err != nil || !response.GetOk() {
			http.Error(w, "unhealthy", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"service":"` + response.GetService() + `"}`))
	})

	// MCP is JSON-RPC 2.0, a different protocol that cannot be a Connect
	// method, so it keeps its own route.
	mux.Handle("/mcp", requireToken(deps.AuthToken, deps.DisableAuth, adaptermcp.NewHandler(deps.Control)))

	if deps.AppFS != nil {
		mux.Handle("/", http.FileServer(http.FS(deps.AppFS)))
	}

	return originGuard(deps, mux)
}
