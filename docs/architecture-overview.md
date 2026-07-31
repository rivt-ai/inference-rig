# Architecture Overview

High-level map of the Go codebase for agents. Each section links to the file
worth opening first — read those rather than re-deriving the structure.

InferenceRig is a **neutral control plane** for local inference engines. One
canonical YAML profile format; one control daemon; several front ends. Engine
specifics (llama.cpp, Apple MLX) live only behind the `backends.Backend`
interface — see `AGENTS.md` for the neutrality rules that constrain every
change.

## The one-paragraph model

A **profile** (YAML on disk) names a backend, a model, and a listen address. The
**control daemon** (`bootstrap.Service`) owns a `control.Manager`, which is the
only place business logic lives. Every front end — CLI, TUI, web UI, MCP —
talks to that manager over the **same Connect/proto `ControlService`**, dialed
over a Unix socket. Starting a profile means: manager asks the backend to
validate + materialize it, gets a `runtime.LaunchSpec`, and hands it to the
generic process **supervisor**.

## Layers

| Layer | Package | Entry point |
| --- | --- | --- |
| Process entry | `main`, `cmd/` | `main.go`, `cmd/root.go:28` (`NewRootCommand`) |
| Daemon assembly | `bootstrap/` | `bootstrap/service.go` (`NewService`) |
| Business logic | `core/control/` | `core/control/manager.go` (`Dependencies`, `Manager`) |
| Wire contract | `core/rpc/` | `core/rpc/proto/inferencerig/control/v1/control.proto` |
| Engine plugins | `backends/` | `backends/contract.go` (`Backend` interface) |
| Front ends | `adapters/` | see below |
| OS primitives | `platform/` | `platform/{pidfile,process,audit,filedoc}` |

Import direction is one-way: `adapters` → `core` → `backends`/`platform`.
Nothing in `core` imports a concrete engine.

## Entry points (`cmd/`)

Subcommands registered in `cmd/root.go:28`:

- `serve` / `daemon` — run the control daemon (`cmd/serve.go`)
- `web` — public HTTP gateway + embedded web app (`cmd/web.go`)
- `tui` — full-screen dashboard; also the bare-`inferencerig` default (`cmd/tui.go`)
- `setup` — first-run wizard (`cmd/setup.go` → `core/setup/wizard.go`)
- `profile` / `model` / `backend` / `runtime` / `events` / `config` groups
  (`adapters/cli/commands.go`)

## Control plane

`core/control/manager.go` defines `Dependencies` — the seams everything else is
injected through (registry, profile store, downloads, signals, audit, catalog,
config store, runtime factory). The manager's public methods are the whole
feature surface; `grep '^func (m \*Manager)' core/control/*.go` is the fastest
inventory. Extra method groups live in `operations.go`, `catalogdownload.go`,
`localmodels.go`, `fit.go`.

`bootstrap/service.go` is the only place these are wired for real, including the
accelerator-telemetry probe assembled from whichever backends implement the
optional `hostResourceProber` facet.

## RPC contract

The proto (`core/rpc/proto/.../control.proto`) is the single wire contract —
~35 unary RPCs plus streams (`WatchEvents`, `WatchModelCatalog`, `WatchLogs`).
Generated code lands in `core/rpc/gen/`; regenerate with `make generate`.

- `core/rpc/control_service.go` — proto handler over `control.Manager`
- `core/rpc/logs_service.go` — separate log/archive service
- `core/rpc/server.go`, `listener.go` — Unix-socket HTTP server
- `core/rpc/transport.go` — `DialControl`, the client every front end uses

**There is no hand-written REST facade.** The browser talks the same Connect
service, re-exported by `adapters/public_http/bridge.go`.

## Backends

`backends/contract.go` is the required reading: `Backend` composes validate →
materialize → launch-spec → resolve → plan → fit → install → capabilities →
catalog policy. Optional facets sit *outside* the interface (e.g.
`RuntimeActivator`, for engines that load a model lazily — llama.cpp's router).

- `backends/registry.go` — registration + `BackendLookup` for the profile store
- `backends/all/register.go` — which backends exist on this host (MLX only on
  darwin/arm64)
- `backends/llamacpp/`, `backends/mlx/` — the two implementations
- `backends/backendtest/` + `backends/contract_test.go` — contract conformance
  suite every backend must pass

## Core domain packages

- `core/profiles/` — canonical profile format, validation, file store
- `core/runtime/supervisor.go` — generic process supervisor: spawn, readiness
  probe, stop, status. Engine-agnostic.
- `core/modelcatalog/` — remote catalog search, local model listing, fit
- `core/modeldownload/` — artifact download jobs
- `core/signals/` — host telemetry (CPU/RAM/disk/accelerator)
- `core/configstore/` — reads/writes the app config file
- `core/setup/` — first-run wizard decisions

## Adapters

- `adapters/cli/commands.go` — cobra command tree over the control client
- `adapters/tui/` — bubbletea dashboard (`run.go` → `model.go` → `ui.go`)
- `adapters/public_http/server.go` — browser gateway: Connect RPC, health, MCP,
  embedded app; auth + origin guard in `auth.go`
- `adapters/mcp/handler.go` — MCP JSON-RPC endpoint exposing control tools

## Config & on-disk layout

`config/config.go` owns the neutral app config and all path resolution
(`ResolvePaths`, `config.go:258`). Everything hangs off `${INFERENCERIG_HOME}`
(default `~/.inferencerig`):

```
config.yaml          profiles/<name>/profile.yaml
models/              cache/hf-catalog/
run/control.sock     run/inferencerig.pid
```

Backend-specific settings live in per-profile YAML, never in `config.Config`.
See `config.example.yaml`.

## Working on this repo

- `make test`, `make lint`, `make verify` — required before committing
- `make generate` (proto), `make webui` (frontend assets)
- `constructor_guard_test.go` enforces one exported `New*` per concrete type
- Integration tests: `test/control_integration_test.go`; end-to-end:
  `test/e2e/`

Related docs: `docs/porting-matrix.md`, `docs/hardware-validation.md`,
`docs/system-coverage-and-e2e-plan.md`, `docs/agents/`.
