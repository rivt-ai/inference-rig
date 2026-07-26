package mcp

import (
	"encoding/json"
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
	{"name": "runtime_start", "description": "Start a profile runtime", "inputSchema": profileSchema()},
	{"name": "runtime_stop", "description": "Stop a profile runtime", "inputSchema": profileSchema()},
	{"name": "runtime_restart", "description": "Restart a profile runtime", "inputSchema": profileSchema()},
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
			write(w, response{JSONRPC: "2.0", ID: req.ID, Error: map[string]any{"code": -32602, "message": err.Error()}})
			return
		}
		write(w, response{JSONRPC: "2.0", ID: req.ID, Result: result})
	})
}

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
		return map[string]any{}, nil
	}
}

func callTool(r *http.Request, client controlv1connect.ControlServiceClient, params callParams) (any, error) {
	calls := []func(*http.Request, controlv1connect.ControlServiceClient, callParams) (proto.Message, bool, error){
		callGeneralTool, callProfileTool, callModelTool, callRuntimeTool,
	}
	var message proto.Message
	var err error
	for _, call := range calls {
		var handled bool
		message, handled, err = call(r, client, params)
		if handled {
			break
		}
	}
	if message == nil && err == nil {
		return nil, &unknownToolError{name: params.Name}
	}
	if err != nil {
		return nil, err
	}
	data, err := protojson.Marshal(message)
	if err != nil {
		return nil, err
	}
	return map[string]any{"content": []map[string]string{{"type": "text", "text": string(data)}}}, nil
}

func callGeneralTool(r *http.Request, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, bool, error) {
	switch params.Name {
	case "backends_list":
		message, err := client.ListBackends(r.Context(), &controlv1.ListBackendsRequest{})
		return message, true, err
	case "backend_install":
		message, err := client.InstallBackend(r.Context(), &controlv1.InstallBackendRequest{
			Backend: stringArg(params, "backend"), Version: stringArg(params, "version"),
		})
		return message, true, err
	case "backend_params":
		message, err := client.GetBackendParams(r.Context(), &controlv1.GetBackendParamsRequest{Backend: stringArg(params, "backend")})
		return message, true, err
	case "info_get":
		message, err := client.GetInfo(r.Context(), &controlv1.GetInfoRequest{})
		return message, true, err
	case "signals_get":
		message, err := client.GetSignals(r.Context(), &controlv1.GetSignalsRequest{})
		return message, true, err
	case "events_list":
		message, err := client.ListEvents(r.Context(), &controlv1.ListEventsRequest{})
		return message, true, err
	case "startup_services_set":
		message, err := client.SetStartupServices(r.Context(), &controlv1.SetStartupServicesRequest{Services: stringsArg(params, "services")})
		return message, true, err
	default:
		return nil, false, nil
	}
}

func callProfileTool(r *http.Request, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, bool, error) {
	name := stringArg(params, "name")
	switch params.Name {
	case "profiles_list":
		message, err := client.ListProfiles(r.Context(), &controlv1.ListProfilesRequest{})
		return message, true, err
	case "profile_get":
		message, err := client.GetProfile(r.Context(), &controlv1.GetProfileRequest{Name: name})
		return message, true, err
	case "profile_put":
		message, err := client.PutProfile(r.Context(), &controlv1.PutProfileRequest{
			Name: name, ProfileYaml: stringArg(params, "profile_yaml"),
		})
		return message, true, err
	case "profile_delete":
		message, err := client.DeleteProfile(r.Context(), &controlv1.DeleteProfileRequest{Name: name})
		return message, true, err
	case "profile_cleanup":
		message, err := client.CleanupProfile(r.Context(), &controlv1.CleanupProfileRequest{Name: name})
		return message, true, err
	case "profile_autostart":
		message, err := client.SetProfileAutostart(r.Context(), &controlv1.SetProfileAutostartRequest{
			Name: name, Enabled: boolArg(params, "enabled"),
		})
		return message, true, err
	default:
		return nil, false, nil
	}
}

func callModelTool(r *http.Request, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, bool, error) {
	switch params.Name {
	case "catalog_search":
		message, err := client.ListModelCatalog(r.Context(), &controlv1.ListModelCatalogRequest{
			Backend: stringArg(params, "backend"), Query: stringArg(params, "query"),
		})
		return message, true, err
	case "models_local":
		message, err := client.ListLocalModels(r.Context(), &controlv1.ListLocalModelsRequest{Backend: stringArg(params, "backend")})
		return message, true, err
	case "model_delete":
		message, err := client.DeleteLocalModel(r.Context(), &controlv1.DeleteLocalModelRequest{
			Backend: stringArg(params, "backend"), Path: stringArg(params, "path"),
		})
		return message, true, err
	case "model_resolve":
		message, err := client.ResolveProfileModel(r.Context(), &controlv1.ResolveProfileModelRequest{Profile: stringArg(params, "profile")})
		return message, true, err
	case "download_start":
		message, err := client.StartModelDownload(r.Context(), &controlv1.StartModelDownloadRequest{Profile: stringArg(params, "profile")})
		return message, true, err
	case "download_get":
		message, err := client.GetModelDownload(r.Context(), &controlv1.GetModelDownloadRequest{Id: stringArg(params, "id")})
		return message, true, err
	case "download_cancel":
		message, err := client.CancelModelDownload(r.Context(), &controlv1.CancelModelDownloadRequest{Id: stringArg(params, "id")})
		return message, true, err
	case "download_apply":
		message, err := client.ApplyDownloadToProfile(r.Context(), &controlv1.ApplyDownloadToProfileRequest{
			Profile: stringArg(params, "profile"), Id: stringArg(params, "id"),
		})
		return message, true, err
	default:
		return nil, false, nil
	}
}

func callRuntimeTool(r *http.Request, client controlv1connect.ControlServiceClient, params callParams) (proto.Message, bool, error) {
	profile := stringArg(params, "profile")
	switch params.Name {
	case "runtime_status":
		message, err := client.GetRuntimeStatus(r.Context(), &controlv1.GetRuntimeStatusRequest{Profile: profile})
		return message, true, err
	case "runtime_start":
		message, err := client.StartRuntime(r.Context(), &controlv1.StartRuntimeRequest{Profile: profile})
		return message, true, err
	case "runtime_stop":
		message, err := client.StopRuntime(r.Context(), &controlv1.StopRuntimeRequest{Profile: profile})
		return message, true, err
	case "runtime_restart":
		message, err := client.RestartRuntime(r.Context(), &controlv1.RestartRuntimeRequest{Profile: profile})
		return message, true, err
	default:
		return nil, false, nil
	}
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

type unknownToolError struct{ name string }

func (e *unknownToolError) Error() string { return "unknown tool: " + e.name }

func objectSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func profileSchema() map[string]any {
	return map[string]any{
		"type": "object", "required": []string{"profile"},
		"properties": map[string]any{"profile": map[string]string{"type": "string"}},
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
