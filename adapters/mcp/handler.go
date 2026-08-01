package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type callParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Result  any    `json:"result,omitempty"`
	Error   any    `json:"error,omitempty"`
}

var tools = []map[string]any{
	{"name": "backends_list", "description": "List available inference backends", "inputSchema": objectSchema()},
	{"name": "backend_install_status", "description": "Get backend installation status", "inputSchema": fieldsSchema("backend")},
	{"name": "backend_install", "description": "Install an inference backend", "inputSchema": fieldsSchema("backend")},
	{"name": "backend_params", "description": "List backend parameters", "inputSchema": fieldsSchema("backend")},
	{"name": "profiles_list", "description": "List canonical profiles", "inputSchema": objectSchema()},
	{"name": "profile_get", "description": "Get a canonical profile", "inputSchema": fieldsSchema("name")},
	{"name": "profile_put", "description": "Create or replace a canonical profile", "inputSchema": fieldsSchema("name", "profile_yaml")},
	{"name": "profile_delete", "description": "Delete a canonical profile", "inputSchema": fieldsSchema("name")},
	{"name": "profile_cleanup", "description": "Delete a profile and unshared local model", "inputSchema": fieldsSchema("name")},
	{"name": "profile_autostart", "description": "Set profile autostart", "inputSchema": fieldsSchema("name", "enabled")},
	{"name": "catalog_search", "description": "Search the remote model catalog", "inputSchema": fieldsSchema("backend")},
	{"name": "models_local", "description": "List local models", "inputSchema": fieldsSchema("backend")},
	{"name": "model_delete", "description": "Delete a local model", "inputSchema": fieldsSchema("backend", "path")},
	{"name": "model_resolve", "description": "Resolve a profile model", "inputSchema": profileSchema()},
	{"name": "download_start", "description": "Start a model download", "inputSchema": profileSchema()},
	{"name": "download_get", "description": "Get a model download", "inputSchema": fieldsSchema("id")},
	{"name": "download_cancel", "description": "Cancel a model download", "inputSchema": fieldsSchema("id")},
	{"name": "download_apply", "description": "Apply a model download to a profile", "inputSchema": fieldsSchema("profile", "id")},
	{"name": "runtime_status", "description": "Get profile runtime status", "inputSchema": profileSchema()},
	{"name": "runtime_start", "description": "Start a profile runtime", "inputSchema": startSchema()},
	{"name": "runtime_stop", "description": "Stop a profile runtime", "inputSchema": profileSchema()},
	{"name": "runtime_restart", "description": "Restart a profile runtime", "inputSchema": profileSchema()},
	{"name": "runtime_reset", "description": "Stop every runtime and clear the active backend", "inputSchema": objectSchema()},
	{"name": "info_get", "description": "Get daemon information", "inputSchema": objectSchema()},
	{"name": "signals_get", "description": "Get host signals", "inputSchema": objectSchema()},
	{"name": "events_list", "description": "List control events", "inputSchema": objectSchema()},
	{"name": "startup_services_set", "description": "Set startup services", "inputSchema": fieldsSchema("services")},
}

// NewHandler returns a small MCP JSON-RPC endpoint backed only by canonical RPC.
func NewHandler(client controlv1connect.ControlServiceClient) http.Handler {
	if client == nil {
		panic("mcp: control client is required")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			write(w, response{JSONRPC: "2.0", Error: map[string]any{"code": -32700, "message": err.Error()}})
			return
		}
		result, err := dispatch(r, client, req)
		if err != nil {
			code := -32602
			if errors.Is(err, errMethodNotFound) {
				code = -32601
			}
			write(w, response{JSONRPC: "2.0", ID: req.ID, Error: map[string]any{"code": code, "message": err.Error()}})
			return
		}
		write(w, response{JSONRPC: "2.0", ID: req.ID, Result: result})
	})
}

// errMethodNotFound distinguishes an unrecognized JSON-RPC method, coded -32601,
// from a malformed call to a known one (-32602).
var errMethodNotFound = errors.New("method not found")

func dispatch(r *http.Request, client controlv1connect.ControlServiceClient, req request) (any, error) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]string{"name": "inferencerig", "version": "1"},
		}, nil
	case "tools/list":
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var params callParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, err
		}
		return callTool(r, client, params)
	default:
		return nil, fmt.Errorf("%w: %s", errMethodNotFound, req.Method)
	}
}

// toolCalls maps each advertised tool to the canonical RPC it performs. A map
// carries no cyclomatic complexity, so the whole surface stays in one table
// rather than being split across dispatchers to satisfy a function-size lint.
var toolCalls = map[string]func(context.Context, controlv1connect.ControlServiceClient, callParams) (proto.Message, error){
	"backends_list": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.ListBackends(ctx, &controlv1.ListBackendsRequest{})
	},
	"backend_install": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.InstallBackend(ctx, &controlv1.InstallBackendRequest{
			Backend: stringArg(params, "backend"), Version: stringArg(params, "version"),
		})
	},
	"backend_install_status": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.GetBackendInstallStatus(ctx, &controlv1.GetBackendInstallStatusRequest{
			Backend: stringArg(params, "backend"),
		})
	},
	"backend_params": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.GetBackendParams(ctx, &controlv1.GetBackendParamsRequest{Backend: stringArg(params, "backend")})
	},
	"info_get": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.GetInfo(ctx, &controlv1.GetInfoRequest{})
	},
	"signals_get": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.GetSignals(ctx, &controlv1.GetSignalsRequest{})
	},
	"events_list": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.ListEvents(ctx, &controlv1.ListEventsRequest{})
	},
	"startup_services_set": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.SetStartupServices(ctx, &controlv1.SetStartupServicesRequest{Services: stringsArg(params, "services")})
	},
	"profiles_list": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.ListProfiles(ctx, &controlv1.ListProfilesRequest{})
	},
	"profile_get": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.GetProfile(ctx, &controlv1.GetProfileRequest{Name: stringArg(params, "name")})
	},
	"profile_put": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.PutProfile(ctx, &controlv1.PutProfileRequest{
			Name: stringArg(params, "name"), ProfileYaml: stringArg(params, "profile_yaml"),
		})
	},
	"profile_delete": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.DeleteProfile(ctx, &controlv1.DeleteProfileRequest{Name: stringArg(params, "name")})
	},
	"profile_cleanup": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.CleanupProfile(ctx, &controlv1.CleanupProfileRequest{Name: stringArg(params, "name")})
	},
	"profile_autostart": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.SetProfileAutostart(ctx, &controlv1.SetProfileAutostartRequest{
			Name: stringArg(params, "name"), Enabled: boolArg(params, "enabled"),
		})
	},
	"catalog_search": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.ListModelCatalog(ctx, &controlv1.ListModelCatalogRequest{
			Backend: stringArg(params, "backend"), Query: stringArg(params, "query"),
		})
	},
	"models_local": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.ListLocalModels(ctx, &controlv1.ListLocalModelsRequest{Backend: stringArg(params, "backend")})
	},
	"model_delete": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.DeleteLocalModel(ctx, &controlv1.DeleteLocalModelRequest{
			Backend: stringArg(params, "backend"), Path: stringArg(params, "path"),
		})
	},
	"model_resolve": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.ResolveProfileModel(ctx, &controlv1.ResolveProfileModelRequest{Profile: stringArg(params, "profile")})
	},
	"download_start": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.StartModelDownload(ctx, &controlv1.StartModelDownloadRequest{Profile: stringArg(params, "profile")})
	},
	"download_get": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.GetModelDownload(ctx, &controlv1.GetModelDownloadRequest{Id: stringArg(params, "id")})
	},
	"download_cancel": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.CancelModelDownload(ctx, &controlv1.CancelModelDownloadRequest{Id: stringArg(params, "id")})
	},
	"download_apply": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.ApplyDownloadToProfile(ctx, &controlv1.ApplyDownloadToProfileRequest{
			Profile: stringArg(params, "profile"), Id: stringArg(params, "id"),
		})
	},
	"runtime_status": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.GetRuntimeStatus(ctx, &controlv1.GetRuntimeStatusRequest{Profile: stringArg(params, "profile")})
	},
	"runtime_start": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.StartRuntime(ctx, &controlv1.StartRuntimeRequest{
			Profile: stringArg(params, "profile"), Replace: boolArg(params, "replace"),
		})
	},
	"runtime_reset": func(ctx context.Context, client controlv1connect.ControlServiceClient, _ callParams) (proto.Message, error) {
		return client.ResetRuntimes(ctx, &controlv1.ResetRuntimesRequest{})
	},
	"runtime_stop": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.StopRuntime(ctx, &controlv1.StopRuntimeRequest{Profile: stringArg(params, "profile")})
	},
	"runtime_restart": func(ctx context.Context, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, error) {
		return client.RestartRuntime(ctx, &controlv1.RestartRuntimeRequest{Profile: stringArg(params, "profile")})
	},
}

func callTool(r *http.Request, client controlv1connect.ControlServiceClient, params callParams) (any, error) {
	call, ok := toolCalls[params.Name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", params.Name)
	}
	message, err := call(r.Context(), client, params)
	if err != nil {
		return nil, err
	}
	data, err := protojson.Marshal(message)
	if err != nil {
		return nil, err
	}
	return map[string]any{"content": []map[string]string{{"type": "text", "text": string(data)}}}, nil
}

func stringArg(params callParams, name string) string {
	value, _ := params.Arguments[name].(string)
	return value
}

func boolArg(params callParams, name string) bool {
	value, _ := params.Arguments[name].(bool)
	return value
}

func stringsArg(params callParams, name string) []string {
	values, _ := params.Arguments[name].([]any)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func objectSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func profileSchema() map[string]any {
	return map[string]any{
		"type": "object", "required": []string{"profile"},
		"properties": map[string]any{"profile": map[string]string{"type": "string"}},
	}
}

// startSchema is profileSchema plus the optional replace flag. An MCP client is
// a client like any other: it cannot terminate a running engine without saying
// so, and this is where it says so.
func startSchema() map[string]any {
	return map[string]any{
		"type": "object", "required": []string{"profile"},
		"properties": map[string]any{
			"profile": map[string]string{"type": "string"},
			"replace": map[string]string{"type": "boolean"},
		},
	}
}

func fieldsSchema(required ...string) map[string]any {
	properties := make(map[string]any, len(required))
	for _, name := range required {
		kind := "string"
		if name == "enabled" {
			kind = "boolean"
		} else if name == "services" {
			properties[name] = map[string]any{"type": "array", "items": map[string]string{"type": "string"}}
			continue
		}
		properties[name] = map[string]string{"type": kind}
	}
	return map[string]any{"type": "object", "required": required, "properties": properties}
}

func write(w http.ResponseWriter, result response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
