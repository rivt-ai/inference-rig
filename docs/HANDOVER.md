# InferenceRig — Build Handover

This document is the single source of truth for the InferenceRig merge build. It
is self-contained: a fresh agent should be able to continue the stacked-PR build
from here **without any further planning stage**. Update the **Current state**
section at the end of every PR.

---

## 1. What InferenceRig is

A neutral local control plane for language-model inference servers with
**pluggable backends** (llama.cpp and Apple MLX). It is assembled in this fresh
repo from two upstream reference implementations — `llamarig` and `mlxrig`,
which are two adapters of one Go template — into a neutral core with all
engine-specific behavior isolated in `backends/llamacpp` and `backends/mlx`.

> The original spec called the project "LocalAiRig" with `~/.localairig` /
> `LOCALAIRIG_*` identifiers. **That naming is stale.** The project is
> **InferenceRig**; use the identifier map in §4 everywhere.

## 2. Locked decisions (from the planning session)

| # | Decision | Choice |
|---|----------|--------|
| 1 | First push | Empty `main` + PR #1 (Phase 0 inventory + Phase 1 bootstrap), then stop for review. **Done.** |
| 2 | `main` contents | LICENSE (MIT) + README stub + Go `.gitignore`. No product code. **Done.** |
| 3 | Branch model | **True stack**: one branch per phase, each PR based on the previous phase's branch, cascading to `main`. |
| 4 | CI | All 5 workflows up front (`test`, `lint`, `live-e2e`, `release`, `dependabot-automerge`), adapted to the current repo state so every PR is gated green. **Done.** |
| 5 | This doc | Captures all context so no further plan stage is needed. |
| 6 | PR draft state | Stacked PRs are opened as **draft** (keeps `live-e2e` skipped until a phase is marked ready). |

### Bootstrap simplifications (ponytail; revisit as noted)

- **Logging**: neutral core uses stdlib `log/slog` (`internal/logs`), not zap. Revisit only if a ported audit component makes slog awkward.
- **Config**: `config.Config` carries **shared fields only**. llama.cpp `RouterConfig` and MLX server fields do **not** belong here — they become per-profile backend config (Phase 4/6/7).
- **Linter**: `.golangci.yml` runs standard linters; the custom module-wide Go **LOC-budget** linter (`goclocbudget`, from `github.com/antonikliment/go-code-metrics`, via `make lint-ci` + `.custom-gcl.yml`) is reinstated in a later phase when there is substantial code to budget.
- **Makefile**: minimal (`build`/`test`/`lint`/`verify` + a no-op `e2e-live`). `generate` (buf), `webui`, real `e2e-live`, and release packaging targets return with their phases.
- **`go` directive**: pinned `go 1.26.3` to match the template. Local `make verify` needs `GOTOOLCHAIN=auto` to fetch the toolchain (verified working in the build container). CI's `setup-go` reads `go-version-file: go.mod`.

## 3. Repository / workspace facts

- Workspace (build container): `/home/user/InferenceRig` (this repo), `/home/user/llamarig`, `/home/user/mlxrig`. **Sources are read-only references** — never develop in them, never merge their git history, never modify them.
- Module path: bare `inferencerig` (matches the templates' bare `module llamarig`).
- Reference commits: llamarig `b66b06f`, mlxrig `f6cab5b` (as of Phase 0).
- **CI is the authority for green** on a PR (`Test` + `Lint` checks). `gofmt` + `go vet` are clean locally; local `golangci-lint` may be too old for the 1.26.3 target — rely on CI's pinned `golangci-lint v2.11.4`.

## 4. Identifier map (neutralize everywhere — spec §6, §11)

```
Go module:       inferencerig
Home dir:        ~/.inferencerig
Config var:      INFERENCERIG_CONFIG
Home var:        INFERENCERIG_HOME
Socket var:      INFERENCERIG_CONTROL_SOCKET
Token var:       INFERENCERIG_CONTROL_TOKEN
App dir var:     INFERENCERIG_APP_DIR
Proto package:   inferencerig.control.v1
Profiles:        ~/.inferencerig/profiles/<name>/profile.yaml
Generated:       ~/.inferencerig/generated/llamacpp/models.ini
```

No `llamarig`/`mlxrig` imports, env names, paths, messages, or engine
terminology may survive in shared packages.

## 5. Stacked-PR map (one branch per phase, based on the previous)

| PR | Branch | Base | Phase | Status |
|----|--------|------|-------|--------|
| 1 | `phase-01-bootstrap` | `main` | 0 + 1 | **this PR** |
| 2 | `phase-02-shared-infra` | `phase-01-bootstrap` | 2 | done |
| 3 | `phase-03-supervisor` | `phase-02-shared-infra` | 3 | done |
| 4 | `phase-04-profiles` | `phase-03-supervisor` | 4 | done |
| 5 | `phase-05-backend-contracts` | `phase-04-profiles` | 5 | done |
| 6 | `phase-06-llamacpp` | `phase-05-backend-contracts` | 6 | done |
| 7 | `phase-07-mlx` | `phase-06-llamacpp` | 7 | done |
| 8 | `phase-08-downloads` | `phase-07-mlx` | 8 | done |
| 9 | `phase-09-control-rpc` | `phase-08-downloads` | 9 | done |
| 10 | `phase-10-interfaces` | `phase-09-control-rpc` | 10 | done |
| 11 | `phase-11-migration` | `phase-10-interfaces` | 11 | done |
| 12 | `phase-12-validation` | `phase-11-migration` | 12 | **this PR** |

Merge cascades bottom-up as each PR is approved. When a lower PR merges to
`main`, rebase/retarget the next open PR's base to `main`.

## 6. How to do the next phase (repeatable recipe)

1. `git fetch origin && git checkout <previous-phase-branch>` (or `main` if it merged).
2. `git checkout -b <next-phase-branch>`.
3. Open `docs/porting-matrix.md`; take the rows tagged for this phase. Read the source packages in `/home/user/llamarig` and `/home/user/mlxrig`.
4. **Port test alongside behavior** (spec §10): identify source tests → port + neutralize → then port production behavior → confirm the new tests pass. The InferenceRig suite is the only authority; do not lean on source test suites.
5. Neutralize all naming (§4). No `llamarig`/`mlxrig` imports in shared packages.
6. Keep commits small and single-purpose; **never mix shared infra and backend behavior in one commit** (spec §9). Each porting commit names the source repo + paths it drew from.
7. `GOTOOLCHAIN=auto make verify` (or at least `go vet ./...` + `go test ./...` + `gofmt -l .`).
8. Commit, `git push -u origin <next-phase-branch>`, open a **draft** PR with base = previous phase branch.
9. Update §5 status + §7 current state in this doc, in the same PR.
10. Meet the phase exit condition (§8) before opening the PR.

## 7. Current state

- `main`: initial empty commit (LICENSE, README, .gitignore). Pushed.
- `phase-01-bootstrap`: Phase 0 porting matrix + Phase 1 neutral bootstrap. Builds green, `go test ./...` passes, no `llamarig`/`mlxrig` imports.
- `phase-02-shared-infra`: Phase 2 shared infrastructure ported + neutralized. Builds green, `go test ./...` passes, no `llamarig`/`mlxrig`/`zap` imports.
- `phase-03-supervisor`: Phase 3 generic process supervisor ported + neutralized. Builds green, `go test ./...` passes (supervisor lifecycle stable at `-count=3`), neutralization grep clean.
- `phase-04-profiles`: Phase 4 canonical YAML profile schema + CRUD store ported + neutralized. Builds green, `go test ./...` passes, `golangci-lint run ./...` = 0 issues, neutralization grep clean.
- `phase-05-backend-contracts`: Phase 5 neutral backend contract seam. Builds green, `go test ./...` passes, `golangci-lint run ./...` = 0 issues, neutralization grep clean.
- `phase-06-llamacpp`: Phase 6 llama.cpp backend. Builds green, `go test ./...` passes, `golangci-lint run ./...` = 0 issues, neutralization grep clean.
- `phase-07-mlx`: Phase 7 MLX backend. Builds green, `go test ./...` passes, `golangci-lint run ./...` = 0 issues, neutralization grep clean.
- `phase-08-downloads`: Phase 8 neutral artifact-plan download executor. Builds green, `go test ./...` passes, `golangci-lint run ./...` = 0 issues, neutralization grep clean.
- `phase-09-control-rpc`: Phase 9 canonical control manager and ConnectRPC service. Builds green, `go test ./...` and `buf lint` pass, `golangci-lint run ./...` = 0 issues, neutralization grep clean.
- `phase-10-interfaces`: Phase 10 canonical-RPC-backed interfaces. Builds green, `go test ./...` and `buf lint` pass, `golangci-lint run ./...` = 0 issues, and no interface imports backend internals.
- `phase-11-migration`: Phase 11 previewable, create-only migration tooling. Builds green, `go test ./...` and `buf lint` pass, `golangci-lint run ./...` = 0 issues, and source immutability is tested.
- `phase-12-validation` (this PR): Phase 12 cross-backend integration and hardware validation. Builds green; `make test`, custom `make lint-ci`, `make e2e-live`, and `buf lint` pass. Hardware tests skip explicitly when their documented engine/model inputs or required host are absent.
- `surface-s1-serve`: runnable control daemon assembly, graceful shutdown, shared PID lifecycle, and CLI daemon commands. See `docs/REMAINING-SURFACES.md` for the outer-surface stack.
- `surface-s2-catalog`: neutral remote catalog transport/cache/refresh module, two backend catalog-policy adapters, safe local model inventory/deletion, and four canonical RPCs.
- `surface-s3-rpc`: completed canonical RPC surface, download provenance/application, safe profile cleanup, neutral autostart config, restart/info, capability-gated backend parameters, and profile-aware runtime slots.
- `surface-s4-cli`: grouped profile/model/backend/runtime/events/config commands cover every canonical RPC, including both server streams, through the generated client only.
- `surface-s5-setup`: llamarig-informed full-screen setup owns local first-run config creation/rerun backups, capability-aware backend installation, and one canonical profile through the generated client.
- `surface-s8-http-mcp`: public HTTP and MCP cover the full unary control surface through the generated client; HTTP mutations retain bearer protection.
- `surface-s6-tui`: llamarig-informed, neutralized tuikit dashboard provides source-style Services/Models/System/Activity views, local control/web service management, multi-backend profile/catalog/local/download workflows, searchable events/logs, and canonical runtime/download/autostart/install actions.
- `surface-s7-webui`: Svelte/Vite SPA scaffolding, runnable web/MCP gateway, pnpm verification, and CI/release frontend builds. The SPA it shipped was a placeholder; the real interface landed on the branch below.
- `claude/llamarig-ui-inferencerig-api-0h13fr`: the gateway serves canonical RPC over Connect with auth failing closed, the contract carries what a UI binds to (fit estimates, host/process/disk telemetry, log RPCs, structured engine args, catalog-first downloads), and the full capability-aware SPA replaces the placeholder. `dist` is CI-built rather than committed.
- **Top of stack:** `claude/llamarig-ui-inferencerig-api-0h13fr`. **Next:** review the completed surface stack bottom-up.
- What exists now (added in Phase 12):
  - `test/control_integration_test.go` — both real backends registered together and driven through one canonical Unix-socket RPC client. It verifies capability discovery, canonical profile creation, runtime lifecycle through the shared factory, backend-specific materialization isolation, and both artifact-plan forms.
  - `test/live` and `make e2e-live` — opt-in real-engine hardware tests that start through the shared supervisor, wait for the backend readiness endpoint, assert running state, and stop cleanly.
  - `docs/hardware-validation.md` — explicit evidence levels, current support matrix, environment inputs, and a record section that prevents unverified hardware claims. The module-wide non-test Go LOC budget is reinstated through the pinned custom linter and CI `make lint-ci`.
- What exists now (added in Phase 11):
  - `core/migrate.Service` — neutral preview/validate/apply orchestration. Plans contain canonical YAML, preview performs no writes, apply creates only missing destination profiles, and repeated application reports existing profiles as skipped rather than replacing them.
  - Backend-owned read-only importers — the single-file backend translates legacy INI sections and cascaded defaults; the directory-artifact backend translates legacy directory YAML while retaining engine arguments and warning about installation/runtime fields that require manual review. Both scan deterministically, reject symlink sources, validate through their real backend and canonical store, and tests assert source bytes remain unchanged.
- What exists now (added in Phase 10):
  - `adapters/cli`, `adapters/mcp`, `adapters/public_http`, and `adapters/tui` — thin interface adapters over the generated canonical control client. CLI commands are wired into the root command; MCP exposes backend/profile/runtime tools; the gateway serves canonical RPC over Connect with mutations authenticated; and the terminal view renders backend/profile/runtime snapshots. Focused fake-client tests prove every interface reaches RPC rather than backend packages.
  - `core/setup.Wizard` — local config-existence first-run detection, terminal/safety checks, full-screen review/cancellation, safe direct config replacement, and canonical profile creation through RPC. Global config never crosses RPC.
  - `webui` — an embedded browser client that talks canonical RPC over Connect and gates its presentation on each backend's advertised capabilities, so unified-memory and multi-file backends render on their own terms rather than llama.cpp's.
- What exists now (added in Phase 9):
  - `core/control.Manager` — the engine-neutral orchestration owner for backend discovery/install, canonical profile CRUD and materialization, runtime lifecycle/status, artifact resolution/downloads, signals, audit, and events. It depends only on the backend registry and shared interfaces; optional batch materialization lets one backend regenerate its complete derived configuration without leaking that format into the core.
  - `inferencerig.control.v1` — one buf-linted canonical protocol with checked-in deterministic Go/Connect generation. `core/rpc.ControlService` maps every manager operation onto that protocol, including backend installation status, event streaming, and stable error codes. A Unix-socket end-to-end test drives profile creation, backend discovery, runtime start, resolution, download completion, signals, and events through the generated client.
- What exists now (added in Phase 8):
  - `core/modeldownload` — one asynchronous download manager consuming only `backends.ArtifactPlan`. It provides queued/running/completed/failed/cancelled/already-downloaded job state, duplicate-active-target coalescing, cancellation, byte/percent progress, expected-size checks, symlink/containment validation, and durable atomic finalization. A single-file plan stages to a sibling `.part` file; a multi-file plan stages the entire directory tree to a sibling `.part` directory and renames it only after every item succeeds. The shape branch is neutral (`ArtifactPlan.MultiFile`), with no backend names or format checks. Tests execute both real plan forms through the same manager and cover nested snapshots, existing targets, duplicate jobs, cancellation, cleanup, and path escape rejection.
  - `backends.ArtifactPlan.TargetRoot` — an explicit neutral atomic-finalization boundary supplied by both backends: the file itself for a single-file plan, or the snapshot directory for a multi-file plan.
- What exists now (added in Phase 7):
  - `backends/mlx` — a real MLX implementation of the same Phase-5 contract used by llama.cpp. It validates canonical YAML profiles and deterministically renders `python -m mlx_lm server` commands, keeping core model/host/port fields reserved while passing sorted scalar/list `engine_args` through safely. Its `LaunchSpec` uses the shared supervisor and `/v1/models` readiness; `Controller` adds the backend-only one-active-profile policy by stopping the current shared supervisor before switching. Hugging Face repositories resolve into containment-checked multi-file snapshot plans, local discovery requires `config.json` plus safetensors weights, fit uses a unified-memory budget, and capabilities advertise multi-file/unified-memory/managed-install/single-active-profile. The managed installer is macOS arm64-gated, creates a private venv, pins/validates `mlx-lm`, persists active state atomically, and is idempotent. The real backend passes `backendtest.RunContractTests`; focused tests cover command rendering, reserved args, launch specs, snapshot resolution/planning/scanning, unified-memory fit, installation, platform gating, switching, and monitoring.
  - `core/modelcatalog.SnapshotScanner` — a neutral directory-artifact counterpart to the Phase-6 file scanner. Shared code owns traversal, cancellation, symlink/staging rejection, and sorting; a backend `DirectoryPolicy` decides snapshot completeness and size.
- What exists now (added in Phase 6):
  - `backends/llamacpp` — the first real implementation of the Phase-5 backend contract, informed by `llamarig` `core/{modelpresets,router,llamainstall,modelcatalog,runtime,signals}` but ported into the engine boundary. Canonical YAML profiles are backend-validated and deterministically rendered to `${INFERENCERIG_HOME}/generated/llamacpp/models.ini`, with a generated-file warning, `version = 1`, optional sorted `[*]` defaults, sorted named sections, canonical llama.cpp keys, injection rejection, and atomic replacement through `platform/filedoc`; invalid input leaves the last valid file untouched. Its `LaunchSpec` runs the shared Phase-3 supervisor against the generated file, model storage, configured listen address, and `/health`. The router client/controller covers list/reload/load/unload plus start/stop/status/recovery through the generic supervisor. Model policy covers local GGUF discovery through the shared catalog scanner, Hugging Face/direct/local resolution, single-file artifact plans, quantization inference, discrete RAM/VRAM fit math, and NVIDIA VRAM probing. The managed installer detects Metal/CUDA/ROCm/Vulkan/CPU, selects official GitHub prebuilt releases, verifies advertised size and SHA-256, activates atomically, is idempotent, and retains active + previous installs. The real backend passes `backendtest.RunContractTests`; focused tests cover deterministic/atomic materialization, launch arguments, router HTTP behavior, resolution/planning, fit, telemetry parsing, install idempotency/upgrade/retention, asset selection, and accelerator priority.
  - `core/modelcatalog` — the neutral Phase-6/7 catalog mechanism introduced with the first backend: a backend-supplied `FormatPolicy`, containment-safe symlink-rejecting local scanner, and shared memory-fit math. It contains no GGUF or llama.cpp terminology; the format and discrete-memory policy stay in `backends/llamacpp`.
  - `config.GeneratedDir(backend)` — the neutral safe-path helper for non-user-owned generated backend output.
- What exists now (added in Phase 5):
  - `backends` — the neutral backend contract seam. The `Backend` interface (`backends/contract.go`) is widened from identity-only into the full contract composing identity + eight facets: `ValidateProfile` (which **is** `profiles.BackendValidator`, compile-time asserted, so the registry can serve as the profile store's `BackendLookup`), `Materialize`, `LaunchSpec` (ties a backend to the Phase-3 generic supervisor), `Resolve`, `Plan`, `Fit`, `Install`, `Capabilities`. Supporting neutral types (`backends/types.go`) are minimal but real, engine-neutral, and extended in later phases: `Materialization`/`GeneratedFile` (rendered runtime form — files and/or summary; MLX's command rides in the LaunchSpec, no engine format leaks); `ResolvedModel`/`ArtifactRef` and `ArtifactPlan`/`ArtifactItem` (cover single-file AND multi-file artifacts with no engine branch; the Phase-8 download executor consumes `ArtifactPlan`); `HostResources` (carries both the discrete-VRAM axis and the unified-memory axis so one type serves both backend families); `FitEstimate`/`FitLevel` (`unknown`/`fits`/`marginal`/`too_large`); `InstallOptions`/`InstallResult` (`Changed` is the idempotency signal); `Capabilities` (single-/multi-file artifacts, discrete-VRAM/unified-memory, managed-install, single-active-profile — for capability gating instead of name branching). `(*Registry).BackendLookup()` (`backends/lookup.go`) adapts the registry to `profiles.BackendLookup`, resolving a key to the registered `Backend` as a `profiles.BackendValidator`; unregistered keys error and the store surfaces them as invalid profiles. The reusable `backends/backendtest` package holds a deterministic engine-free `Fake` implementing the full contract plus `RunContractTests(t, newBackend)` — the cross-backend contract suite (non-empty `Name`; `ValidateProfile` accepts valid / rejects structurally-bad; `Materialize`+`LaunchSpec` yield a usable spec or surfaced `BuildErr`; `Resolve`→`Plan` yields a non-empty plan whose artifact form matches `Capabilities`; `Fit` returns a valid level; `Install` is idempotent; `Capabilities` self-consistent) — so Phases 6/7 run their real backends through it verbatim. `TestRegistryBackedProfileStore` proves a `profiles.FileStore` built with the registry's `BackendLookup` performs full CRUD against a registered backend through interfaces only. No engine-specific behavior in `backends/` — interfaces, neutral types, registry, adapter, and the reusable suite only.
- What exists now (added in Phase 4):
  - `core/profiles` — the canonical YAML profile schema + engine-agnostic CRUD store. The neutral `Profile` schema owns the common fields (`version`, `name`, `backend`, `model.{source,reference}`, `listen.{host,port}`) and keeps `engine_args` a free-form `map[string]any` the backend owns; decoded with `yaml.v3` `KnownFields(true)` so unknown top-level keys are rejected. `FileStore` (directory-per-profile at `<root>/<name>/profile.yaml` under `config.ProfilesDir()`) provides `List`/`Get`/`Validate`/`Create`/`Replace`/`Delete` with path-escape + symlink rejection (`filedoc.RejectSymlink`), size limits, atomic writes (`filedoc.WriteFile`), `filedoc.SyncDir` on delete, and a mutex — reusing Phase-2 `platform/filedoc`, not reimplementing it. Shared common-field validation (name matches dir, version/name/backend required, `listen.port` 1..65535, `model.source` required) runs first; **engine_args validation is delegated to a `BackendValidator` interface** (`ValidateProfile(Profile) (Profile, error)`) resolved via an injected `BackendLookup func(backend string) (BackendValidator, error)` — the store hardcodes no engine and an unknown backend is rejected as invalid. `Effective` = profile after shared normalization + backend validation; `Parsed` = raw decode. No command/models.ini rendering here (Phases 6/7). The real `backends` registry is wired in Phase 5; a `fakeBackend` in tests drives the full CRUD lifecycle to prove engine-independence.
- What exists now (added in Phase 3):
  - `core/runtime` — generic process `Supervisor` + `LaunchSpec` (the neutral union of the two engine configs). Unifies the two ~95%-identical engine runtimes (`llamarig` `LlamaServer`, `mlxrig` `MLXServer`) into ONE neutral supervisor: process-group start, PID-file bookkeeping, TCP/HTTP readiness probing + timeout, graceful SIGINT stop with `StopTimeout`→SIGKILL escalation, `cmd.Wait`/`done` goroutine, adopt-on-recover (executable match, stale/mismatch rejection), unsafe-PID-name rejection, injectable clock/HTTP client. No engine defaults, no `config` engine-type import (PIDDir is caller-supplied). `LaunchSpec.BuildErr` lets a backend defer a command-render failure to `Start`. Backends supply the `LaunchSpec` in Phases 6/7 (the engine `build.go` builders are NOT ported here). Fake-process tests (`TestMain` re-exec helper) cover the full lifecycle with no real engine.
- What exists now (added in Phase 2):
  - `platform/filedoc` — atomic file write/replace (+ backup, symlink rejection, SHA-256).
  - `platform/pidfile` — PID file create/read/stale-detect (gopsutil executable match).
  - `platform/process` — detached start/status/stop + serve-loop supervisor over `log/slog`.
  - `platform/audit` — log tailing/archive/retention primitives + an audit `Sink` over `log/slog` (no zap).
  - `core/runtime` — neutral runtime state/error **types only** (`State`/`Status`/`ProcessStatus`/`CommandResult`/error kinds). The generic supervisor is Phase 3.
  - `core/control` — neutral error taxonomy (`ErrorKind`/`Errorf`/`CoreError`/`MapSentinel`), `AuditEvent`/`AuditSink`, and the in-memory `EventStore`. The control daemon/Manager is Phase 9.
  - `core/configstore` — generic config.yaml persistence (`FileStore` over `filedoc`) + `SetStartupServices`. Canonical YAML profile store is Phase 4.
  - `core/signals` — shared host RAM/CPU/disk/process telemetry (gopsutil). GPU/VRAM & unified-memory are backend policy (Phase 6/7).
  - `core/rpc` — unix-socket control transport helpers (`NewControlListener`, `ControlTransport`, HTTP `Server` wrapper, `ValidateRequestInterceptor`, control-kind↔connect-code bridge), decoupled from the generated proto service (Phase 9). `core/rpc/gen` stays gitignored.
  - go.mod added `connectrpc.com/connect` + `github.com/shirou/gopsutil/v4` (protobuf indirect); no zap, no flock.
- Carried from Phase 1: `config` (neutral), `cmd` (root + version), `internal/buildinfo`, `internal/logs`, `backends` (empty registry + contract-test seed), `constructor_guard_test.go`, Makefile, `.golangci.yml`, `config.example.yaml`, all 5 CI workflows, `.github/dependabot.yml`. Structural dirs `adapters/ webui/ test/` remain placeholders (`.gitkeep`).

## 8. Phase exit conditions (spec §8)

- **0 Inventory** — every major source package has a destination or an explicit no-port decision. (`docs/porting-matrix.md`.)
- **1 Bootstrap** — builds + minimal green test suite, no source-module imports.
- **2 Shared infra** — `platform/{filedoc,pidfile,process}`, runtime state/errors, audit/events, config persistence, unix-socket transport ported + independently tested; no duplicate llamarig/mlxrig copies.
- **3 Supervisor** — fake-process tests cover the full lifecycle without a real engine; backends provide a `LaunchSpec`.
- **4 Profiles** — canonical YAML profiles created/validated/replaced/listed/deleted through shared interfaces; a fake backend proves CRUD is engine-independent.
- **5 Backend contracts** — shared control logic runs entirely against backend interfaces; fake backend passes contract tests.
- **6 llama.cpp** — manage llama.cpp entirely through YAML while the router consumes a generated `models.ini`.
- **7 MLX** — install/start/stop/switch/monitor MLX profiles through the same shared APIs as llama.cpp.
- **8 Downloads** — shared code executes both artifact forms (single GGUF / MLX snapshot) with no backend-specific branches.
- **9 Control RPC** — both backends fully controllable through one canonical RPC service.
- **10 Interfaces** — every interface (CLI/MCP/HTTP/TUI/Web) calls canonical RPC; none call backend internals.
- **11 Migration** — importers are repeatable, previewable, non-destructive; never mutate source installs.
- **12 Validation** — shared contract + real-engine tests; documented, verified support levels (MLX on Apple Silicon; llama.cpp on supported OS/accelerators).

## 9. Definition of success (spec §11)

- No runtime dependency on the llamarig/mlxrig Go modules; deleting the sources from the workspace still leaves InferenceRig building.
- Shared packages contain no llama.cpp/MLX imports; backend packages hold all engine-specific behavior.
- Canonical profiles are YAML. llama.cpp receives a generated `models.ini`; MLX receives a generated command. Both engines use the **same** supervisor, control service, profile CRUD, and download infra.
- InferenceRig's own tests verify all retained behavior. Migration tools read source state without modifying it.

---

## Appendix A — Full merge spec (verbatim)

The authoritative spec this build follows. (Project renamed LocalAiRig →
InferenceRig; `localairig`/`LOCALAIRIG_*`/`~/.localairig` → the §4 identifiers.)

### 1. Repository strategy
InferenceRig is implemented in a new, empty repository. LlamaRig and MLXRig are
upstream reference implementations; they remain separate and unchanged. Do not
begin by copying either source wholesale — build the neutral architecture first,
then port selected packages into their intended final locations.

### 2. Source-repository rules
Treat LlamaRig and MLXRig as read-only inputs. **May**: inspect, port, compare,
preserve useful behavior/tests, refactor copied code into neutral packages,
record origin of substantial copied components, preserve license/attribution.
**Must not**: develop directly in a source repo; merge one repo's git history
into the other; modify sources to prepare the merge; retain `llamarig`/`mlxrig`
module imports in the final shared core; copy one entire repo and gradually
rename it; treat either package layout as automatically authoritative; preserve
engine-specific terminology in shared packages merely because it exists upstream.

### 3. Architectural objective
Neutral control plane with pluggable backends. **Shared control plane**:
RPC/HTTP/MCP, CLI/TUI/web, canonical YAML profiles, model catalog, model
downloads, telemetry, audit and events, configuration, generic process
supervisor. **Backends** (`llamacpp`, `mlx`) each: controller, profile
validator, command/models.ini renderer, catalog policy, artifact planner, fit
estimator, installer (+ router client for llamacpp). The shared control plane
must not depend directly on llama.cpp or MLX packages; backend packages may
depend on shared interfaces/infrastructure.

### 4. Canonical profile direction
YAML is the only user-managed profile format, stored at
`~/.inferencerig/profiles/<profile-name>/profile.yaml`. Every profile declares
its `backend` and common fields (`version`, `name`, `model.{source,reference}`,
`listen.{host,port}`, `engine_args`). The shared profile package owns common
fields; the selected backend validates and interprets `engine_args`.

### 5. llama.cpp YAML materialization
For the llama.cpp backend, canonical YAML profiles are rendered into a generated
`models.ini`: YAML → shared parsing → llama.cpp validation → effective
defaults/overrides → deterministic `models.ini` rendering → atomic replacement →
router source refresh. Generate at
`~/.inferencerig/generated/llamacpp/models.ini` (not user-owned; carries a
"generated, do not edit" warning header with `version = 1`). Backend-wide
defaults render into `[*]`; each profile into a named section. Generation is
deterministic and atomic; invalid profiles never replace the last valid file.

### 6. New repository bootstrap
First milestone is the neutral repository, not engine support. Create
`go.mod`, `main.go`, `Makefile`, `README.md`, `LICENSE`, and dirs `config/`,
`core/`, `backends/`, `adapters/`, `platform/`, `webui/`, `docs/`, `test/`.
Neutral identifiers from the start (§4). Use the final module path; never
temporarily use `llamarig`/`mlxrig`.

### 7. Porting classification
Before porting a file, classify it: **A** shared infrastructure (atomic file
ops, PID handling, process exec, runtime state/error types, logging/audit, event
storage, ConnectRPC unix-socket transport, download job state) → neutral
packages, strip project specifics. **B** shared behavior requiring abstraction
(process supervision, model download execution, catalog caching, runtime
orchestration, profile CRUD, host/process telemetry) → shared mechanism, policy
behind interfaces. **C** llama.cpp code (router lifecycle/client, `models.ini`
rendering, GGUF resolution/quantization, discrete RAM/VRAM fit, llama.cpp
install) → `backends/llamacpp`. **D** MLX code (`mlx_lm.server` command
construction, one-active-profile switching, MLX snapshot detection,
unified-memory fit, managed Python env) → `backends/mlx`.

### 8. Implementation phases
0 Source inventory (porting matrix; no porting before it exists). 1 Bootstrap
the fresh repo. 2 Port exact shared infrastructure. 3 Generic process
supervisor (backends provide a `LaunchSpec`). 4 Canonical YAML profiles +
storage (fake backend proves independence). 5 Backend contracts + registry (fake
backend contract tests). 6 llama.cpp backend (YAML→generated `models.ini`). 7
MLX backend (same shared APIs). 8 Shared download engine (plans, no backend
branches). 9 Canonical control service (`inferencerig.control.v1`). 10 User
interfaces (CLI→MCP→HTTP→TUI→Web, capability-gated). 11 Migration tooling
(read-only importers). 12 Integration + hardware validation. (Exit conditions
mirrored in §8 above.)

### 9. Commit strategy
Small reviewable commits. Each porting commit states which source repo + paths
informed it. Avoid commits that introduce shared infrastructure and backend
behavior simultaneously.

### 10. Testing rule for ported code
Port tests alongside behavior: identify source tests → port + neutralize → add
cross-backend contract tests where appropriate → port/implement production
behavior → confirm new tests pass. Do not rely on the source repo's suite as
proof. The fresh repo's suite is the authority.

### 11. Definition of architectural success
(See §9 above — no llamarig/mlxrig runtime dep; sources removable; shared
packages engine-free; backends hold engine behavior; YAML profiles; generated
`models.ini` for llama.cpp; generated command for MLX; one supervisor / control
service / profile CRUD / download infra for both; InferenceRig tests verify all
retained behavior; migration reads source state without modifying it.)
