package public_http

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	adaptermcp "inferencerig/adapters/mcp"
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
//
//nolint:funlen // Keeping the declarative route registry together preserves route/auth locality.
func NewHandler(deps Dependencies) http.Handler {
	if deps.Control == nil {
		panic("public_http: control client is required")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.Health(r.Context(), &controlv1.HealthRequest{})
	}))
	mux.Handle("/mcp", authorize(deps.AuthToken, adaptermcp.NewHandler(deps.Control)))
	mux.HandleFunc("GET /api/backends", rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.ListBackends(r.Context(), &controlv1.ListBackendsRequest{})
	}))
	mux.HandleFunc("GET /api/profiles", rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.ListProfiles(r.Context(), &controlv1.ListProfilesRequest{})
	}))
	mux.HandleFunc("GET /api/info", rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.GetInfo(r.Context(), &controlv1.GetInfoRequest{})
	}))
	mux.HandleFunc("GET /api/profiles/{name}", rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.GetProfile(r.Context(), &controlv1.GetProfileRequest{Name: r.PathValue("name")})
	}))
	mux.Handle("PUT /api/profiles/{name}", authorize(deps.AuthToken, rpcResponse(func(r *http.Request) (proto.Message, error) {
		request := &controlv1.PutProfileRequest{}
		if err := decodeProto(r, request); err != nil {
			return nil, err
		}
		request.Name = r.PathValue("name")
		return deps.Control.PutProfile(r.Context(), request)
	})))
	mux.Handle("DELETE /api/profiles/{name}", authorize(deps.AuthToken, rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.DeleteProfile(r.Context(), &controlv1.DeleteProfileRequest{Name: r.PathValue("name")})
	})))
	mux.Handle("POST /api/profiles/{name}/cleanup", authorize(deps.AuthToken, rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.CleanupProfile(r.Context(), &controlv1.CleanupProfileRequest{Name: r.PathValue("name")})
	})))
	mux.Handle("POST /api/profiles/{name}/autostart", authorize(deps.AuthToken, rpcResponse(func(r *http.Request) (proto.Message, error) {
		request := &controlv1.SetProfileAutostartRequest{}
		if err := decodeProto(r, request); err != nil {
			return nil, err
		}
		request.Name = r.PathValue("name")
		return deps.Control.SetProfileAutostart(r.Context(), request)
	})))
	mux.HandleFunc("GET /api/catalog", rpcResponse(func(r *http.Request) (proto.Message, error) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		return deps.Control.ListModelCatalog(r.Context(), &controlv1.ListModelCatalogRequest{
			Backend: r.URL.Query().Get("backend"), Query: r.URL.Query().Get("query"), Limit: int32(limit),
		})
	}))
	mux.HandleFunc("GET /api/models/local", rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.ListLocalModels(r.Context(), &controlv1.ListLocalModelsRequest{Backend: r.URL.Query().Get("backend")})
	}))
	mux.Handle("DELETE /api/models/local", authorize(deps.AuthToken, rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.DeleteLocalModel(r.Context(), &controlv1.DeleteLocalModelRequest{
			Backend: r.URL.Query().Get("backend"), Path: r.URL.Query().Get("path"),
		})
	})))
	mux.HandleFunc("GET /api/models/resolve/{profile}", rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.ResolveProfileModel(r.Context(), &controlv1.ResolveProfileModelRequest{Profile: r.PathValue("profile")})
	}))
	mux.Handle("POST /api/downloads/{profile}", authorize(deps.AuthToken, rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.StartModelDownload(r.Context(), &controlv1.StartModelDownloadRequest{Profile: r.PathValue("profile")})
	})))
	mux.HandleFunc("GET /api/downloads/{id}", rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.GetModelDownload(r.Context(), &controlv1.GetModelDownloadRequest{Id: r.PathValue("id")})
	}))
	mux.Handle("POST /api/downloads/{id}/cancel", authorize(deps.AuthToken, rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.CancelModelDownload(r.Context(), &controlv1.CancelModelDownloadRequest{Id: r.PathValue("id")})
	})))
	mux.Handle("POST /api/downloads/{id}/apply/{profile}", authorize(deps.AuthToken, rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.ApplyDownloadToProfile(r.Context(), &controlv1.ApplyDownloadToProfileRequest{
			Id: r.PathValue("id"), Profile: r.PathValue("profile"),
		})
	})))
	mux.Handle("POST /api/backends/{backend}/install", authorize(deps.AuthToken, rpcResponse(func(r *http.Request) (proto.Message, error) {
		request := &controlv1.InstallBackendRequest{}
		if err := decodeProto(r, request); err != nil {
			return nil, err
		}
		request.Backend = r.PathValue("backend")
		return deps.Control.InstallBackend(r.Context(), request)
	})))
	mux.HandleFunc("GET /api/backends/{backend}/install", rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.GetBackendInstallStatus(r.Context(), &controlv1.GetBackendInstallStatusRequest{Backend: r.PathValue("backend")})
	}))
	mux.HandleFunc("GET /api/backends/{backend}/params", rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.GetBackendParams(r.Context(), &controlv1.GetBackendParamsRequest{Backend: r.PathValue("backend")})
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
	mux.Handle("POST /api/runtime/{profile}/restart", authorize(deps.AuthToken, rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.RestartRuntime(r.Context(), &controlv1.RestartRuntimeRequest{Profile: r.PathValue("profile")})
	})))
	mux.HandleFunc("GET /api/signals", rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.GetSignals(r.Context(), &controlv1.GetSignalsRequest{})
	}))
	mux.HandleFunc("GET /api/events", rpcResponse(func(r *http.Request) (proto.Message, error) {
		return deps.Control.ListEvents(r.Context(), &controlv1.ListEventsRequest{})
	}))
	mux.Handle("PUT /api/config/startup", authorize(deps.AuthToken, rpcResponse(func(r *http.Request) (proto.Message, error) {
		request := &controlv1.SetStartupServicesRequest{}
		if err := decodeProto(r, request); err != nil {
			return nil, err
		}
		return deps.Control.SetStartupServices(r.Context(), request)
	})))
	if deps.AppFS != nil {
		files := http.FileServer(http.FS(deps.AppFS))
		mux.Handle("/", files)
	}
	return mux
}

func decodeProto(r *http.Request, message proto.Message) error {
	const limit = 2 << 20
	data, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return err
	}
	if len(data) > limit {
		return errors.New("request body exceeds 2 MiB")
	}
	return protojson.Unmarshal(data, message)
}

func rpcResponse(call func(*http.Request) (proto.Message, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response, err := call(r)
		if err != nil {
			status := http.StatusBadGateway
			if connect.CodeOf(err) == connect.CodeInvalidArgument {
				status = http.StatusBadRequest
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
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
