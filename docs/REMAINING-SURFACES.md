# Remaining Surfaces — post-Phase-12 completion plan

The twelve-phase stack delivered a **complete, neutral control core** with two real
backends, proven end-to-end in `test/control_integration_test.go`. What is *not*
yet delivered is the **outer surface tier**: a runnable daemon, the model-catalog
capability, the full canonical RPC surface, and full-fat user interfaces. This
document inventories those gaps and lays out an actionable, dependency-ordered
plan to close them.

Status legend: ✅ complete · 🟡 partial/thin · ❌ missing.

## Delivery stack

Each sequence ships as its own stacked draft PR:

| Sequence | Branch | Base |
|---|---|---|
| S1 | `surface-s1-serve` | `phase-12-validation` |
| S2 | `surface-s2-catalog` | `surface-s1-serve` |
| S3 | `surface-s3-rpc` | `surface-s2-catalog` |
| S4 | `surface-s4-cli` | `surface-s3-rpc` |
| S5 | `surface-s5-setup` | `surface-s4-cli` |
| S8 | `surface-s8-http-mcp` | `surface-s5-setup` |
| S6 | `surface-s6-tui` | `surface-s8-http-mcp` |
| S7 | `surface-s7-webui` | `surface-s6-tui` |

## 1. What is already done (do not re-port)

- ✅ Neutral core: `platform/*`, `core/{runtime,profiles,configstore,control,rpc,modeldownload,modelcatalog(scanner+fit only),migrate,setup,signals}`.
- ✅ Backend contract seam (`backends`) + both real backends (`backends/llamacpp`, `backends/mlx`) passing `backendtest.RunContractTests`.
- ✅ Canonical `inferencerig.control.v1` RPC (17 methods) + `core/control.Manager`.
- ✅ Thin adapters that *reach* RPC: `adapters/{cli,mcp,public_http,tui}`, embedded `webui` shell.
- ✅ Read-only migration, integration + live test harness, hardware-validation doc, `goclocbudget` LOC budget.

## 2. Ground rules (carried from the phased build — every workstream obeys these)

1. **Read-only sources.** Port + neutralize from `/home/user/{llamarig,mlxrig}`; never modify them.
2. **Neutralize naming (HANDOVER §4).** No `llamarig`/`mlxrig` imports, env names, paths, or engine terms in shared packages. Engine terms are allowed only inside `backends/{llamacpp,mlx}`.
3. **Contract discipline.** Engine behavior stays behind the Phase-5 backend interfaces / catalog-policy seams; shared code never imports an engine package. Interfaces call **only** canonical RPC, never backend internals.
4. **Test alongside behavior** (spec §10); run new backends/policies through the existing contract suites.
5. **Gate every change:** `gofmt -l .`, `go vet ./...`, `go test ./...`, and the custom `make lint-ci` (`goclocbudget` included) must all be clean. The module-wide non-test Go LOC budget is temporarily 15,000; report and continue if that budget alone fails while the remaining gates pass.
6. Small, single-purpose commits; each porting commit names its source paths.

## 3. Gap inventory

### Canonical RPC surface: 17 present vs 26 in source. Missing capabilities:

| Missing RPC (source name → neutral name) | Capability | Notes |
|---|---|---|
| `ListModelCatalog`, `WatchModelCatalog` | **Model catalog browse/search** | ✅ shared HTTP/cache/refresh module with backend-owned artifact policy |
| `ListLocalModels`, `DeleteLocalModel` | **Local model management** | ✅ backend-owned scanning and containment-checked deletion through canonical RPC |
| `ApplyModelDownloadToPreset` → `ApplyDownloadToProfile` | Wire a finished download into a profile | ✅ validates download provenance and completion before canonical YAML replacement |
| `CleanupPreset` → `CleanupProfile` | Reclaim a profile's model artifacts | ✅ rejects shared artifacts, removes the profile, then reclaims its local artifact |
| `SetPresetAutostart` → `SetProfileAutostart`, `SetStartupServices` | Autostart / startup-services config | ✅ neutral config mutations exposed through RPC |
| `RestartRuntime` | Restart (have start+stop only) | ✅ manager-owned stop/start sequence |
| `GetInfo` | App/system info | ✅ profiles/backends/runtimes/config/build snapshot |
| `GetLlamaServerParams` → capability-gated `GetBackendParams` | Backend-specific param introspection | ✅ optional advertised backend facet |

### Interface tier

| Surface | State | Source LOC | Ours |
|---|---|---|---|
| **`serve` / daemon command** | ✅ complete — `bootstrap.Service` owns assembly/lifecycle; `serve` and `daemon {status,stop}` are wired | — | — |
| **Model catalog (shared client)** | ✅ complete | `core/modelcatalog/{huggingface.go(645),local.go,cache.go,url.go,quant.go,events.go}` | neutral HTTP/cache/events + scanners/policies |
| **CLI** | ✅ complete — grouped commands reach every canonical RPC, including streams | `adapters/cli` 434 | full neutral surface |
| **Setup wizard command** | ✅ capability-aware stdlib prompts create canonical YAML through RPC | — | — |
| **TUI** | ✅ interactive multi-view dashboard over canonical RPC | `adapters/tui` ~2,300 (+tabs,+ui) | lean Bubble Tea model |
| **Web UI** | ✅ Svelte/Vite SPA, pnpm verification, committed embedded dist, CI + release builds | `webui/web` 158 `.svelte` + vite/pnpm pipeline | neutral REST client |
| **public_http (REST facade)** | ✅ full unary surface with bearer protection on mutations | `adapters/public_http` 734 | neutral generated-client facade |
| **MCP** | ✅ full unary tool surface through generated client | `adapters/mcp` ~440 | neutral tool registry |

## 4. Workstreams (dependency-ordered)

### S1 — Runnable daemon (`serve`) — ✅ complete
Without this nothing runs; every other surface needs a live control socket.
- Add a `bootstrap` package (or `cmd/serve.go`) that assembles what `test/control_integration_test.go` already builds: `backends.NewRegistry()` → `llamacpp.Register` + `mlx.Register` → `profiles.NewFileStore(..., registry.BackendLookup())` → `control.NewManager(...)` → `rpc.NewControlService` → `rpc.NewServer` → serve on the Unix socket, with graceful shutdown + PID/lock via `platform/pidfile`.
- Add `inferencerig serve` (and likely `inferencerig status`/`stop`) to `cmd/root.go`.
- **Exit:** `inferencerig serve` starts a socket the existing CLI verbs talk to; a smoke test starts the daemon in-process and drives one RPC. Source ref: the integration test + `llamarig` app bootstrap/startup wiring.

### S2 — Model catalog capability — ✅ complete
- Port `llamarig/core/modelcatalog/{huggingface.go,local.go,cache.go,url.go,events.go}` into the **shared** `core/modelcatalog` as a neutral search/list/cache mechanism, with the GGUF-vs-snapshot **format policy behind the existing backend catalog-policy seam** (extend `FormatPolicy`; MLX snapshot policy already exists). Keep all HF/GGUF specifics in `backends/{llamacpp,mlx}`.
- Add manager methods + RPCs: `ListModelCatalog`/`WatchModelCatalog`, `ListLocalModels`, `DeleteLocalModel`.
- **Exit:** browse/search catalog + list/delete local models for *both* backends through one neutral mechanism; policy stays in backends. Tests over a recorded HTTP fixture (no live network).

### S3 — Complete the canonical RPC surface — ✅ complete
- Add the remaining manager methods + proto RPCs (regen deterministically, `buf lint`): `ApplyDownloadToProfile`, `CleanupProfile`, `SetProfileAutostart`, `SetStartupServices`, `RestartRuntime`, `GetInfo`, and capability-gated `GetBackendParams`.
- **Exit:** RPC surface reaches functional parity with source (neutralized); each method covered by a generated-client test. Depends on S2 for the catalog/local pieces.

### S4 — CLI breadth — ✅ complete
- Surface the full RPC set as verbs: `profile {create,edit,delete,get,list}`, `model {search,list,download,cancel,resolve,rm}`, `backend {list,install,params}`, `runtime {start,stop,restart,status}`, `signals`, `events [--watch]`. Port structure from `llamarig/adapters/cli` (neutralized).
- **Exit:** every canonical RPC reachable from the CLI; fake-client tests prove each verb hits RPC only. Depends on S1+S3.

### S5 — Setup wizard command — ✅ complete
- Wire `core/setup.Wizard` into `inferencerig setup` (capability-gated prompts, creates a canonical profile via RPC only).
- **Exit:** interactive setup produces a valid profile through RPC. Depends on S1.

### S6 — Full TUI — ✅ complete
- Port `llamarig/adapters/tui` (+tabs, +ui, ~2,300 LOC) over the generated canonical client — backend/profile/runtime/catalog/download/events views. Neutralize; no backend imports.
- **Exit:** interactive TUI drives the full control surface through RPC. Large. Depends on S1+S3+S2.

### S7 — Full Web UI — ✅ complete
- Port the `webui/web` Svelte/Vite/pnpm app (158 `.svelte`) + build pipeline; Go embeds `webui/dist`; wire `vite build` into `make webui` and CI. Frontend talks only to the `public_http` facade → canonical RPC.
- **Exit:** `make webui` builds the real SPA; embedded assets served; `make build` stays green without a node toolchain (committed `dist` or CI-built). Large. Depends on S1+S8.

### S8 — public_http + MCP breadth — ✅ complete
- Expand the REST facade (`adapters/public_http`, source 734) and MCP tools (`adapters/mcp`, source ~440) to cover the full canonical surface (catalog, downloads, local models, profile CRUD, setup), keeping auth + capability-gating. The web UI (S7) consumes this.
- **Exit:** REST + MCP expose the full surface; every handler reaches RPC only. Depends on S1+S3.

## 5. Suggested sequencing

```
S1 (serve) ──┬── S3 (RPC surface) ──┬── S4 (CLI)
             │                      ├── S8 (HTTP+MCP) ── S7 (Web UI)
             ├── S2 (catalog) ──────┘
             └── S5 (setup)              S6 (TUI)  ← after S2/S3
```

S1 first (unblocks all). S2 and S3 are the functional heart (catalog + full RPC).
S4/S5/S8 are moderate adapter work. S6 (TUI) and S7 (Web UI) are the two large
UI ports and can run last / in parallel once the RPC surface is complete.

## 6. Rough sizing

| Workstream | Size | Risk |
|---|---|---|
| S1 serve/daemon | S | low — lift from integration test |
| S2 model catalog | L | med — HTTP client, cache, policy seam, fixtures |
| S3 RPC surface | M | low — mechanical, deterministic regen + `buf lint` |
| S4 CLI breadth | M | low |
| S5 setup command | S | low |
| S6 full TUI | XL | med — large stateful UI port |
| S7 full Web UI | XL | med — frontend toolchain + embed/CI |
| S8 HTTP+MCP breadth | M | low |

## 7. Definition of done (surface tier)

- `inferencerig serve` runs the daemon; every interface (CLI/TUI/HTTP/MCP/Web) drives the **full** canonical surface through it, none importing backend internals.
- Model catalog browse/search + local-model management work for both backends through one neutral mechanism.
- Canonical RPC reaches neutralized parity with the source surface.
- `make build`, `make test`, `make lint-ci` (incl. `goclocbudget`), `make webui`, `buf lint` all green; neutralization grep clean in shared packages.
