package mcp

import (
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
	{"name": "profiles_list", "description": "List canonical profiles", "inputSchema": objectSchema()},
	{"name": "runtime_status", "description": "Get profile runtime status", "inputSchema": profileSchema()},
	{"name": "runtime_start", "description": "Start a profile runtime", "inputSchema": profileSchema()},
	{"name": "runtime_stop", "description": "Stop a profile runtime", "inputSchema": profileSchema()},
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

// errMethodNotFound distinguishes an unrecognized JSON-RPC method, which the
// spec codes as -32601, from a malformed call to a known one (-32602).
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

func callTool(r *http.Request, client controlv1connect.ControlServiceClient, params callParams) (any, error) {
	profile, _ := params.Arguments["profile"].(string)
	var (
		message proto.Message
		err     error
	)
	switch params.Name {
	case "backends_list":
		message, err = client.ListBackends(r.Context(), &controlv1.ListBackendsRequest{})
	case "profiles_list":
		message, err = client.ListProfiles(r.Context(), &controlv1.ListProfilesRequest{})
	case "runtime_status":
		message, err = client.GetRuntimeStatus(r.Context(), &controlv1.GetRuntimeStatusRequest{Profile: profile})
	case "runtime_start":
		message, err = client.StartRuntime(r.Context(), &controlv1.StartRuntimeRequest{Profile: profile})
	case "runtime_stop":
		message, err = client.StopRuntime(r.Context(), &controlv1.StopRuntimeRequest{Profile: profile})
	default:
		return nil, fmt.Errorf("unknown tool: %s", params.Name)
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

func objectSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func profileSchema() map[string]any {
	return map[string]any{
		"type": "object", "required": []string{"profile"},
		"properties": map[string]any{"profile": map[string]string{"type": "string"}},
	}
}

func write(w http.ResponseWriter, result response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
