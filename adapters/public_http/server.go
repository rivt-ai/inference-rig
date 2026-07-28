package public_http

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"inferencerig/core/control"
	"inferencerig/core/rpc"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

// Dependencies configures the public REST facade.
type Dependencies struct {
	Control   controlv1connect.ControlServiceClient
	AuthToken string
	AppFS     fs.FS
}

// NewHandler returns an HTTP facade whose operations all use canonical RPC.
func NewHandler(deps Dependencies) http.Handler {
	if deps.Control == nil {
		panic("public_http: control client is required")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.Health(r.Context(), &controlv1.HealthRequest{})
	}))
	mux.HandleFunc("GET /api/backends", rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.ListBackends(r.Context(), &controlv1.ListBackendsRequest{})
	}))
	mux.HandleFunc("GET /api/profiles", rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.ListProfiles(r.Context(), &controlv1.ListProfilesRequest{})
	}))
	mux.HandleFunc("GET /api/runtime/{profile}", rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.GetRuntimeStatus(r.Context(), &controlv1.GetRuntimeStatusRequest{Profile: r.PathValue("profile")})
	}))
	mux.Handle("POST /api/runtime/{profile}/start", authorize(deps.AuthToken, rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.StartRuntime(r.Context(), &controlv1.StartRuntimeRequest{Profile: r.PathValue("profile")})
	})))
	mux.Handle("POST /api/runtime/{profile}/stop", authorize(deps.AuthToken, rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.StopRuntime(r.Context(), &controlv1.StopRuntimeRequest{Profile: r.PathValue("profile")})
	})))
	if deps.AppFS != nil {
		files := http.FileServer(http.FS(deps.AppFS))
		mux.Handle("/", files)
	}
	return mux
}

// httpStatus maps a control error kind onto its HTTP status. The kind survives
// the RPC hop via rpc.ErrorKindFromRPC, so a caller's mistake is reported as
// such instead of every failure surfacing as an upstream fault.
func httpStatus(err error) int {
	switch rpc.ErrorKindFromRPC(err) {
	case control.ErrorInvalidInput:
		return http.StatusBadRequest
	case control.ErrorPermission:
		return http.StatusForbidden
	case control.ErrorNotFound:
		return http.StatusNotFound
	case control.ErrorConflict:
		return http.StatusConflict
	case control.ErrorTimeout:
		return http.StatusGatewayTimeout
	default:
		return http.StatusBadGateway
	}
}

func rpcResponse(call func(*http.Request) (proto.Message, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response, err := call(r)
		if err != nil {
			writeJSON(w, httpStatus(err), map[string]string{"error": err.Error()})
			return
		}
		data, err := protojson.Marshal(response)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}
}

func authorize(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") != token {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authorization required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "encode response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
