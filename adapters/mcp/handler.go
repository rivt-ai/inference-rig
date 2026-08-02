package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

// client is the canonical RPC surface every tool goes through. Aliased because
// the dispatch table below names one of its methods per row, and the row is
// easier to read than the interface's full name is.
type client = controlv1connect.ControlServiceClient

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
	{"name": "backends_list", "description": "List available inference backends", "inputSchema": fieldsSchema()},
	{"name": "backend_install_status", "description": "Get backend installation status", "inputSchema": fieldsSchema("backend")},
	{"name": "backend_install", "description": "Install an inference backend", "inputSchema": fieldsSchema("backend")},
	{"name": "backend_params", "description": "List backend parameters", "inputSchema": fieldsSchema("backend")},
	{"name": "profiles_list", "description": "List canonical profiles", "inputSchema": fieldsSchema()},
	{"name": "profile_get", "description": "Get a canonical profile", "inputSchema": fieldsSchema("name")},
	{"name": "profile_put", "description": "Create or replace a canonical profile", "inputSchema": fieldsSchema("name", "profile_yaml")},
	{"name": "profile_delete", "description": "Delete a canonical profile", "inputSchema": fieldsSchema("name")},
	{"name": "profile_cleanup", "description": "Delete a profile and unshared local model", "inputSchema": fieldsSchema("name")},
	{"name": "profile_autostart", "description": "Set profile autostart", "inputSchema": fieldsSchema("name", "enabled")},
	{"name": "catalog_search", "description": "Search the remote model catalog", "inputSchema": fieldsSchema("backend")},
	{"name": "models_local", "description": "List local models", "inputSchema": fieldsSchema("backend")},
	{"name": "model_delete", "description": "Delete a local model", "inputSchema": fieldsSchema("backend", "path")},
	{"name": "model_resolve", "description": "Resolve a profile model", "inputSchema": fieldsSchema("profile")},
	{"name": "download_start", "description": "Start a model download", "inputSchema": fieldsSchema("profile")},
	{"name": "download_get", "description": "Get a model download", "inputSchema": fieldsSchema("id")},
	{"name": "download_cancel", "description": "Cancel a model download", "inputSchema": fieldsSchema("id")},
	{"name": "download_apply", "description": "Apply a model download to a profile", "inputSchema": fieldsSchema("profile", "id")},
	{"name": "runtime_status", "description": "Get profile runtime status", "inputSchema": fieldsSchema("profile")},
	{"name": "runtime_start", "description": "Start a profile runtime", "inputSchema": startSchema()},
	{"name": "runtime_stop", "description": "Stop a profile runtime", "inputSchema": fieldsSchema("profile")},
	{"name": "runtime_restart", "description": "Restart a profile runtime", "inputSchema": fieldsSchema("profile")},
	{"name": "runtime_reset", "description": "Stop every runtime and clear the active backend", "inputSchema": fieldsSchema()},
	{"name": "info_get", "description": "Get daemon information", "inputSchema": fieldsSchema()},
	{"name": "signals_get", "description": "Get host signals", "inputSchema": fieldsSchema()},
	{"name": "events_list", "description": "List control events", "inputSchema": fieldsSchema()},
	{"name": "startup_services_set", "description": "Set startup services", "inputSchema": fieldsSchema("services")},
}

// NewHandler returns a small MCP JSON-RPC endpoint backed only by canonical RPC.
func NewHandler(rpc client) http.Handler {
	if rpc == nil {
		panic("mcp: control client is required")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			write(w, response{JSONRPC: "2.0", Error: map[string]any{"code": -32700, "message": err.Error()}})
			return
		}
		result, err := dispatch(r, rpc, req)
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

func dispatch(r *http.Request, rpc client, req request) (any, error) {
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
		return callTool(r, rpc, params)
	default:
		return nil, fmt.Errorf("%w: %s", errMethodNotFound, req.Method)
	}
}

// call is one tools/call invocation: the request context, the client, and the
// tool's own arguments.
type call struct {
	ctx       context.Context
	rpc       client
	arguments map[string]any
}

// bind turns one generated client method into a tool. The tool's arguments are
// decoded straight into that method's request message, because MCP argument
// names are the request's proto field names: a hand-written translation per tool
// would be twenty-seven copies of what protojson already does, each of them free
// to disagree with the schema the tool advertises.
//
// Unknown arguments are discarded rather than rejected, so a client that sends
// more than a tool documents still gets the call it asked for.
func bind[Req any, PtrReq interface {
	*Req
	proto.Message
}, Resp proto.Message](method func(client, context.Context, PtrReq) (Resp, error)) func(call) (proto.Message, error) {
	return func(c call) (proto.Message, error) {
		arguments, err := json.Marshal(c.arguments)
		if err != nil {
			return nil, err
		}
		request := PtrReq(new(Req))
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(arguments, request); err != nil {
			return nil, err
		}
		return method(c.rpc, c.ctx, request)
	}
}

// toolCalls maps each advertised tool to the canonical RPC it performs. A map
// carries no cyclomatic complexity, so the whole surface stays in one table
// rather than being split across dispatchers to satisfy a function-size lint.
var toolCalls = map[string]func(call) (proto.Message, error){
	"backends_list":          bind(client.ListBackends),
	"backend_install":        bind(client.InstallBackend),
	"backend_install_status": bind(client.GetBackendInstallStatus),
	"backend_params":         bind(client.GetBackendParams),
	"info_get":               bind(client.GetInfo),
	"signals_get":            bind(client.GetSignals),
	"events_list":            bind(client.ListEvents),
	"startup_services_set":   bind(client.SetStartupServices),
	"profiles_list":          bind(client.ListProfiles),
	"profile_get":            bind(client.GetProfile),
	"profile_put":            bind(client.PutProfile),
	"profile_delete":         bind(client.DeleteProfile),
	"profile_cleanup":        bind(client.CleanupProfile),
	"profile_autostart":      bind(client.SetProfileAutostart),
	"catalog_search":         bind(client.ListModelCatalog),
	"models_local":           bind(client.ListLocalModels),
	"model_delete":           bind(client.DeleteLocalModel),
	"model_resolve":          bind(client.ResolveProfileModel),
	"download_start":         bind(client.StartModelDownload),
	"download_get":           bind(client.GetModelDownload),
	"download_cancel":        bind(client.CancelModelDownload),
	"download_apply":         bind(client.ApplyDownloadToProfile),
	"runtime_status":         bind(client.GetRuntimeStatus),
	"runtime_start":          bind(client.StartRuntime),
	"runtime_stop":           bind(client.StopRuntime),
	"runtime_restart":        bind(client.RestartRuntime),
	"runtime_reset":          bind(client.ResetRuntimes),
}

func callTool(r *http.Request, rpc client, params callParams) (any, error) {
	invoke, ok := toolCalls[params.Name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", params.Name)
	}
	// A tool called with no arguments decodes to a nil map, which marshals to
	// null rather than to the empty request it means.
	if params.Arguments == nil {
		params.Arguments = map[string]any{}
	}
	message, err := invoke(call{ctx: r.Context(), rpc: rpc, arguments: params.Arguments})
	if err != nil {
		return nil, err
	}
	data, err := protojson.Marshal(message)
	if err != nil {
		return nil, err
	}
	return map[string]any{"content": []map[string]string{{"type": "text", "text": string(data)}}}, nil
}

// argumentTypes gives the JSON type of every argument that is not a string.
var argumentTypes = map[string]map[string]any{
	"enabled":  {"type": "boolean"},
	"replace":  {"type": "boolean"},
	"services": {"type": "array", "items": map[string]string{"type": "string"}},
}

// fieldsSchema is the input schema of a tool taking exactly the named
// arguments, all of them required. Called with none it is the empty object
// schema, which is what a tool that takes no arguments advertises.
func fieldsSchema(required ...string) map[string]any {
	properties := make(map[string]any, len(required))
	for _, name := range required {
		kind, ok := argumentTypes[name]
		if !ok {
			kind = map[string]any{"type": "string"}
		}
		properties[name] = kind
	}
	return map[string]any{"type": "object", "required": required, "properties": properties}
}

// startSchema is the profile schema plus the optional replace flag. An MCP
// client is a client like any other: it cannot terminate a running engine
// without saying so, and this is where it says so.
func startSchema() map[string]any {
	schema := fieldsSchema("profile")
	schema["properties"].(map[string]any)["replace"] = argumentTypes["replace"]
	return schema
}

func write(w http.ResponseWriter, result response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
