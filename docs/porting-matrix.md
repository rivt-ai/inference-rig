# Porting Matrix (Phase 0)

Inventory of the two read-only reference repos (`llamarig`, `mlxrig`) mapped to
their intended InferenceRig destinations. **No code is ported before a package
appears here.** Classification (spec §7):

- **A — Shared infrastructure**: port verbatim into neutral packages; strip project names/paths/env/messages.
- **B — Shared behavior needing abstraction**: port the common mechanism; push engine policy behind backend interfaces.
- **C — llama.cpp backend**: port only into `backends/llamacpp`.
- **D — MLX backend**: port only into `backends/mlx`.
- **discard**: do not port.

The two repos are the *same Go template*; unless noted, a shared row means
"unify the two near-identical implementations into one neutral package — do not
keep both."

## A — Shared infrastructure → neutral packages

| Source (both repos unless noted) | Destination | Tests | Naming changes | Notes |
|---|---|---|---|---|
| `platform/filedoc` | `platform/filedoc` | yes | none (already neutral) | Atomic file write/replace. Underpins profile store + models.ini rendering. |
| `platform/pidfile` | `platform/pidfile` | yes | none | PID file create/read/stale-detect. |
| `platform/process` | `platform/process` | yes | none | Process exec/group/signal helpers. Supervisor builds on this. |
| `platform/audit` | `platform/audit` | yes | strip project strings | Audit log primitives. |
| `core/signals` (primitives) | `core/signals` | yes | drop NVIDIA/unified-mem specifics into backend telemetry | RAM/CPU host telemetry is shared; GPU/VRAM vs unified-memory axis is backend policy (B). |
| `internal/buildinfo` | `internal/buildinfo` | — | none | **Ported (PR #1).** |
| `config` (neutral subset) | `config` | yes | `LLAMARIG_*`/`MLX_*`→`INFERENCERIG_*`; drop `RouterConfig`/MLX server fields | **Ported (PR #1).** Engine config moves to per-profile YAML. |

## B — Shared behavior requiring abstraction

| Source | Destination | Phase | Tests | Notes |
|---|---|---|---|---|
| `core/runtime` (both) | `core/runtime` generic supervisor | 3 | fake-process lifecycle | Backends supply a `LaunchSpec`; supervisor owns start/stop/status/recovery/PID/probes/timeout/kill. Two impls are the behavioral reference. |
| `core/configstore` | `core/configstore` / profile store | 4 | yes | Persistence mechanism is shared; canonical store is neutral YAML (`profiles/<name>/profile.yaml`), not llamarig's `models.ini` and not mlxrig's layout. |
| `core/modeldownload` | `core/modeldownload` engine | 8 | yes | Generic executor over backend-generated plans (1 file GGUF / snapshot MLX). Job IDs, queue, cancel, progress, staging, atomic finalize — no engine branches. |
| `core/modelcatalog` | `core/modelcatalog` | 6/7 | yes | Caching/ranking shared; format policy (GGUF vs MLX repo) behind a backend `catalog policy` interface. |
| `core/control` | `core/control` daemon | 9 | yes | Canonical `inferencerig.control.v1` RPC over registry/profiles/runtime/catalog/downloads/signals/events. |
| `core/setup` | `core/setup` | 10 | yes | Setup wizard; neutral prompts, backend-aware sections via capabilities. |
| `bootstrap` | `bootstrap` | 1–9 | — | App wiring/DI. Grows as services land; keep neutral. |
| `core/rpc` transport helpers | `core/rpc` | 2 | yes | ConnectRPC Unix-socket transport is shared infra (A-like); canonical proto/service is Phase 9. Generated `core/rpc/gen` stays gitignored. |

## C — llama.cpp backend → `backends/llamacpp`

| Source (`llamarig`) | Destination | Phase | Notes |
|---|---|---|---|
| `core/router` | `backends/llamacpp` (controller + HTTP client + recovery) | 6 | Router lifecycle, load/unload/reload/list. |
| `core/modelpresets` | `backends/llamacpp` (parsing + `models.ini` renderer) | 6 | Port **only** parsing/rendering knowledge for materialization + migration — NOT as the canonical store. YAML→deterministic, atomic `~/.inferencerig/generated/llamacpp/models.ini`. |
| `core/llamainstall` | `backends/llamacpp` installer | 6 | Managed llama.cpp install/upgrade, backend detection. |
| GGUF resolution / discrete RAM+VRAM fit (in router/catalog) | `backends/llamacpp` (catalog policy + fit + single-file artifact plan) | 6 | |

## D — MLX backend → `backends/mlx`

| Source (`mlxrig`) | Destination | Phase | Notes |
|---|---|---|---|
| `core/mlxcmd` | `backends/mlx` command renderer | 7 | `mlx_lm.server` command construction from canonical profile. |
| `core/serverconfigs` | `backends/mlx` (validation) + canonical schema input | 7 | MLX YAML profiles inform the canonical schema; port validation, not a second store. |
| `core/mlxinstall` | `backends/mlx` installer | 7 | Managed Python venv. |
| One-active-profile switching (in `core/runtime`/control) | `backends/mlx` controller | 7 | MLX serves one model/process; switching stops current, starts new. |
| MLX snapshot detection / unified-memory fit | `backends/mlx` (catalog policy + fit + multi-file artifact plan) | 7 | |

## Interfaces / UI (Phase 5, 10)

| Source (both) | Destination | Phase | Notes |
|---|---|---|---|
| `adapters/cli` | `adapters/cli` | 10 | Neutral actions; backend-specific only when capabilities advertise. |
| `adapters/mcp` | `adapters/mcp` | 10 | |
| `adapters/public_http` | `adapters/public_http` | 10 | App-owned REST facade over canonical RPC. |
| `adapters/tui` (+tabs, +ui) | `adapters/tui` | 10 | |
| `webui` | `webui` | 10 | Svelte/Vite/pnpm; Go embeds `webui/dist`. |
| backend registry + contracts (new) | `backends` | 5 | **Empty registry ported (PR #1);** contracts (runtime/validate/materialize/resolve/plan/fit/install/discovery) added Phase 5. |

## Migration (Phase 11)

| Source input | Destination | Notes |
|---|---|---|
| llamarig `models.ini` | `core/migrate` (or `backends/llamacpp/migrate`) | Sections→canonical YAML; `[*]`→backend defaults; unknown keys→`engine_args`. Read-only. |
| mlxrig profile YAML | canonical profile schema | Preserve MLX args. Read-only. |

## Discard / not ported as-is

- Per-repo `README.md`, `LICENSE` (InferenceRig has its own), `logos/`, `docker/` (revisit in a packaging phase).
- Either repo's **whole** control plane / adapter layout as authoritative — rebuild neutral, port behavior into it (spec §2).
- Duplicate second copy of any shared package (unify, don't keep both).
