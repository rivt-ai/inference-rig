# MCP

InferenceRig exposes its control plane as MCP JSON-RPC over HTTP. Start both
services, then connect an MCP client to the gateway:

```sh
infr serve --detach
infr web
```

The default endpoint is `http://127.0.0.1:7000/mcp`.

## Authentication

Send the gateway token as a bearer token:

```text
Authorization: Bearer <token>
```

`infr web` prints the token URL. A generated token is also stored at
`~/.inferencerig/run/gateway.token`. Set `INFERENCERIG_CONTROL_TOKEN` before
starting the gateway to supply your own token.

## Example

List the available tools:

```sh
curl http://127.0.0.1:7000/mcp \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

Call a tool:

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": {
    "name": "runtime_start",
    "arguments": { "profile": "dev" }
  }
}
```

Tool results contain the canonical control response as JSON text.

## Tools

| Area | Tools and arguments |
| --- | --- |
| Backends | `backends_list()`, `backend_install_status(backend)`, `backend_install(backend)`, `backend_params(backend)` |
| Profiles | `profiles_list()`, `profile_get(name)`, `profile_put(name, profile_yaml)`, `profile_delete(name)`, `profile_cleanup(name)`, `profile_autostart(name, enabled)` |
| Models | `catalog_search(backend)`, `models_local(backend)`, `model_delete(backend, path)`, `model_resolve(profile)` |
| Downloads | `download_start(profile)`, `download_get(id)`, `download_cancel(id)`, `download_apply(profile, id)` |
| Runtimes | `runtime_status(profile)`, `runtime_start(profile, replace?)`, `runtime_stop(profile)`, `runtime_restart(profile)`, `runtime_reset()` |
| System | `info_get()`, `signals_get()`, `events_list()`, `startup_services_set(services)` |

`enabled` and `replace` are booleans. `services` is a string list containing
`control`, `web`, or both.

`runtime_start` does not stop an existing profile unless `replace` is `true`.
Delete, cleanup, reset, stop, install, and configuration tools change local
state; clients should confirm those calls with the user.
