# InferenceRig Roadmap

Date: 2026-08-03

Milestones A (beta reliability) and B (release readiness) from this document's
prior form are delivered: fail-fast config, backend concurrency and runtime
state machine, recovery/reconciliation, operational autostart, gateway
security, download persistence, engine install/rollback hardening, evidence
correction, multi-platform release packaging, SBOM, build provenance, a real
MLX release gate, packaged-artifact E2E, and a stable/dev install script with
provenance verification. What that work actually delivered is in
[`CONTEXT.md`](../CONTEXT.md) and [`hardware-validation.md`](hardware-validation.md)
— this file only tracks what is left.

## Not yet done

- **Model-serving observability.** Operation IDs, events and audit logging
  exist (`core/control/events.go`, `audit_sink.go`, `journal.go`), but no
  metrics do: the codebase has no identifier for time to first token,
  throughput, queue depth or load duration. OpenTelemetry appears in `go.mod`
  transitively via `buf` only — nothing imports it, so there is no
  observability stack to keep or remove.

  Two findings re-shape this item — see the session notes for detail:
  - **Inference traffic does not pass through InferenceRig.** The public
    gateway (`adapters/public_http/server.go`) serves control RPC, `/health`,
    `/mcp` and static files; clients talk to the engine's own port directly.
    There is no request path of ours to instrument, so throughput, queue depth
    and TTFT cannot be obtained by adding timers to our code.
  - **llama.cpp already computes most of it.** `llama-server --metrics` gives
    throughput and queue depth for the cost of one launch flag and a scrape,
    which is far cheaper than inserting a reverse proxy into the serving path.

  Remaining requirements stand: "not measured" kept distinct from a measured
  zero, bounded history, and a structured export that doesn't require standing
  up an observability stack. `EventStore` already satisfies the last two and
  should be the template rather than a new mechanism.
- **Release receipt.** Deliberately not built — the packaged-artifact E2E and
  MLX CI runs are the evidence trail; nothing today consumes a separate
  machine-readable receipt format.
- **Hardware detection and explainable model selection.** Done, with two
  caveats noted below. The neutral architecture was already in place:
  `backends.HostResources` (`backends/types.go`) carries both the
  discrete-VRAM and unified-memory axes in one type; `acceleratorProbe`
  (`bootstrap/service.go`) asks each registered backend for its own probe with
  no branching on backend names; and fit policy is per-backend
  (`backends/llamacpp/fit.go` discrete, `backends/mlx/fit.go` unified).

  What shipped:
  - AMD probe (`rocm-smi`, `backends/llamacpp/host.go`), tried only when NVIDIA
    finds nothing, alongside the existing NVIDIA probe.
  - CPU-only as an explicit named state (`AcceleratorName = "CPU"`) instead of
    an empty accelerator slice.
  - Multi-GPU capacity policy: the sum is kept (matches how llama.cpp splits
    layers across devices), but the pooled name now discloses the device count
    (`"2× ..."`) instead of reading as one device — see the caveat below.
  - Context- and quantization-aware sizing. `core/modelcatalog/gguf.go` is a
    new from-scratch GGUF header parser (no dependency existed or was added)
    reading only the KV-table keys sizing needs. `core/modelcatalog/fit.go`
    gained `KVCacheBytes`/`RequiredBytes`, wired into
    `backends/llamacpp/fit.go` via `p.EngineArgs["ctx-size"]` and a resolved
    local model file, cached by mtime (`backends/llamacpp/archcache.go`). Only
    applies when the model is already downloaded (both in `ListModelCatalog`
    and profile-based `EstimateFit`); not-yet-downloaded catalog variants keep
    the flat `size + 512 MiB` estimate, disclosed as such in the reason.
  - Explanations carried through to the user via the existing `Verdict.Reason`
    seam — no proto or UI change needed, since `FitEstimate.Reason` and
    `AcceleratorName` already reached clients.

  Caveats, both unverified against real hardware in this session:
  - The KV-cache formula is the standard transformer estimate, believed to
    match llama.cpp's cache layout, but not checked against llama.cpp source
    or a measured allocation.
  - `rocm-smi`'s `--showmeminfo vram --showproductname --json` flags and key
    names (`"VRAM Total Memory (B)"`, `"Card series"`) are from public
    documentation, not exercised against a real ROCm install.
- **Model switchboard validation.** Proving, after a switch, that the endpoint
  reports the new model identity, that streaming/cancellation work, that the
  previous model is unloaded or intentionally retained, and that rollback
  works.
- **Lifecycle diagnostics.** Reinstall, restart, recovery and `doctor` flows
  deepened further — this is more valuable right now than another inference
  backend.
- **Hardware-aware defaults.** Recommended quantization, context length,
  batch/parallelism, GPU offload policy, memory headroom and readiness
  timeout — visible, explainable and overrideable in canonical profiles.
- **Backup, export and migration.** Versioned export/import for profiles,
  config, model inventory metadata, engine install records and audit/
  validation receipts. No model bytes unless explicitly requested.
- **Support matrix with evidence levels**, tracked separately: code-supported
  platforms, CI-tested platforms, hardware-validated platforms, released
  binary targets, experimental accelerator paths. Never present interface/
  contract tests as hardware proof.

## Triage (2026-08-03)

Ballpark sizing to prioritise the list above. The release receipt is excluded —
it is a deliberate non-goal, not pending work. Sizes are estimates, not
measurements; the hardware-detection row is the only one grounded in reading
the code.

| Item | Complexity | Value | Notes |
| --- | --- | --- | --- |
| Model-serving observability | Low–Medium if TTFT is dropped; High if not | High | Re-scoped after reading the code. There is no request path of ours to instrument, so the cheap route is scraping `llama-server --metrics` rather than timing our own code. Three of the four metrics come almost free that way; TTFT is not exposed and needs a reverse proxy, which is most of the cost. `EventStore` already provides the ring buffer and subscribe shape. |
| Hardware detection + selection | Low for detection; Medium overall | High | Re-scoped after reading the code — see the revised bullet above and the session notes. The neutral architecture already exists; remaining detection work is small. The cost is in sizing accuracy, not detection. |
| Switchboard validation | Low–Medium | Medium–High | Mostly tests against existing behaviour. Value spikes if it finds real bugs; unload leaks and rollback gaps are common in this area. Risk: tests may expose work that grows the item. |
| Lifecycle diagnostics | Medium, unbounded | Medium–High | No natural finish line. Needs a fixed checklist of `doctor` checks agreed up front or it sprawls. |
| Hardware-aware defaults | Medium | High | Arithmetic plus policy tables. Gated on hardware detection; near-pointless without it. Explainability is cheap if designed in from the start, expensive if retrofitted. |
| Backup / export / migration | Low–Medium | Medium | Well-understood shape: collect known state, version-stamped archive, import with validation. Main cost is enumerating everything that counts as state without missing any. Low risk. |
| Support matrix | Low | Medium–High | Mostly a doc plus a discipline rule. Cheapest item here, and it directly serves the project's own "never present contract tests as hardware proof" principle. |

Best value per unit of effort: support matrix, observability, switchboard
validation, backup/export. Biggest bet: hardware detection and hardware-aware
defaults as a pair. Needs scoping before it starts: lifecycle diagnostics.

## Features to keep outside the core

- RAG databases and ingestion pipelines
- autonomous-agent frameworks
- general workflow automation
- speech and image-generation stacks
- full homelab orchestration
- general-purpose observability suites

External systems may consume stable inference endpoints without becoming
responsibilities of the core.

## Milestone D — extensibility (past this effort)

1. Stabilize backend capability and concurrency contracts.
2. Publish a backend authoring guide and contract-test kit.
3. Add a compatibility/version policy for external backends.
4. Consider additional engines only after recovery and release gates are
   proven — they are, as of Milestone B.

## Session notes (2026-08-03)

Findings from a triage session, recorded so they are not re-derived. Nothing
here has been acted on.

### Observability: what exists, and the cheap route

**Nothing metric-shaped exists.** No identifier in the codebase for TTFT,
throughput, queue depth, latency, histograms or load duration. OpenTelemetry is
in `go.mod` only as a transitive dependency of `buf`; no file imports it.

**Model-load time is, in effect, already measured.** `Manager.transition`
(`core/control/slot.go`) records `Duration: time.Since(op.start)` on every
runtime transition, with `op.start` set when the slot is claimed. The
transition to ready therefore already carries elapsed start time. Note this is
a superset of model load — it includes process spawn and readiness polling, not
just weight loading — so it should be labelled honestly if surfaced. Checking
whether this is good enough is the cheapest possible first step on this item.

**`EventStore` is the template to copy** (`core/control/events.go`). It is
already a bounded ring buffer (`DefaultEventLimit = 200`, evicting from the
front), JSON-tagged, with `SubscribeAndList` handing subscribers a backlog plus
a non-blocking channel. That is the roadmap's "bounded history and structured
export without an observability stack" requirement, already solved once. A
metrics store should reuse this shape rather than introduce a second mechanism.

**We are not in the request path.** `adapters/public_http/server.go` (90 lines)
serves control RPC, `GET /health`, `/mcp` and static files.
`backends/llamacpp/launch.go` starts the engine on the profile's own listen
port and clients connect to it directly. Consequently throughput, queue depth
and TTFT are not obtainable by instrumenting our own code — the choice is
inserting a reverse proxy into the serving path (adding latency and a new
failure mode) or scraping the engine.

**Scraping is much cheaper.** Verified against the installed
`llama-server --help` and the llama.cpp `tools/server/README.md` metrics table:

| Roadmap metric | Source |
| --- | --- |
| Throughput | `llamacpp:predicted_tokens_seconds`, `llamacpp:prompt_tokens_seconds` (gauges) |
| Queue depth | `llamacpp:requests_deferred` plus `llamacpp:requests_processing` (gauges) |
| Model-load time | Already present via `Event.Duration`, see above |
| Time to first token | **Not exposed.** `prompt_seconds_total` is prompt-processing time — a proxy, not TTFT |

Also available: `prompt_tokens_total`, `prompt_seconds_total`,
`tokens_predicted_total`, `tokens_predicted_seconds_total`, `n_tokens_max`,
`n_decode_total`, `n_busy_slots_per_decode`.

Two constraints, both verified rather than assumed:
- `--metrics` defaults to **disabled**. It would be added in
  `backends/llamacpp/launch.go` next to the existing `--models-*` arguments.
- We run llama.cpp in **router mode** (`--models-preset`, `--models-max`). The
  llama.cpp README states `/metrics` in router mode requires a
  `?model={model_id}` query parameter and returns 400 without it, so the scrape
  must be per-model rather than one global fetch.
- `--slots` is enabled by default and is a second source, giving per-slot state.

This is llama.cpp-specific and must not leak into shared code as a branch on a
backend name. It belongs behind a backend-contributed facet, the same way
`hostResourceProber` works for accelerators.

**Suggested split:** ship throughput and queue depth via scraping, confirm
whether the existing `Event.Duration` covers load time, and record TTFT as
deliberately deferred — it is the only one of the four that requires sitting in
the request path, and carrying that cost for one metric is a poor trade.

### Suspected bug: multi-GPU VRAM is summed

`probeNVIDIAVRAM` (`backends/llamacpp/host.go`) sums `memory.total` across all
NVIDIA GPUs into a single figure. Two 12 GB cards therefore report 24 GB, and a
20 GB model is reported as fitting when it will not load unless the backend
splits layers across devices.

Summing is right for capacity accounting and wrong for a single-model fit
verdict. Not yet confirmed against real multi-GPU hardware — verify before
treating it as a defect. Independent of any new feature work.

### Open questions

1. **Multi-GPU policy.** Resolved: keep the sum (matches how llama.cpp splits
   layers across devices) and disclose the device count in the pooled name
   (`backends/llamacpp/host.go`) rather than change the capacity model. Not
   verified against real multi-GPU hardware.
2. **Is GGUF metadata already parsed anywhere?** Resolved: no. Written from
   scratch in `core/modelcatalog/gguf.go` — a KV-table-only reader, no
   dependency, stops before the tensor table.
3. **Is TTFT worth a reverse proxy?** It is the only one of the four serving
   metrics that cannot be scraped. Answering no makes the observability item
   substantially cheaper.
4. **Does the existing `Event.Duration` on the ready transition already answer
   "how long did the model take to load"** well enough to close that part of
   the item without new code? It measures spawn plus readiness, not weight
   loading alone.

### Rejected: `jaypipes/ghw` for VRAM detection

Evaluated as a plug-in hardware-detection library. Rejected: per its
documentation, `ghw.GPU()` exposes `Index`, `Address`, `DeviceInfo` (PCI
vendor/product/class) and `Node` — **no VRAM field**. VRAM capacity is the only
value the fit math consumes, so the whole probe would still have to be written
by hand, with an added dependency and SBOM entry alongside it. Roughly 30 lines
of Linux sysfs reading gets the number ghw cannot supply.

Also relevant: releases build `CGO_ENABLED=0` (`scripts/package-release.sh`),
which independently rules out `NVIDIA/go-nvml` and other cgo-based vendor SDK
bindings. Shelling out to vendor CLIs — the current approach — is the right
call under that constraint. Whether ghw itself is cgo-free was not verified;
its docs do not state it.

Worth reconsidering ghw only for the **support matrix** item, where full PCI
enumeration ("which GPUs exist, including ones no backend can use") is the
actual goal and hand-rolling cross-platform PCI walking is not worth it.

### Note on `nvidia-smi`

Each probe spawns a subprocess. If probe latency becomes a problem, cache the
static fields (device name, total VRAM) rather than switching to NVML — the
cgo constraint above still applies.
