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
  exist (`core/control/events.go`, `audit_sink.go`, `journal.go`), but
  model-load time, time to first token, throughput and queue depth are not
  reported anywhere. Needs "not measured" kept distinct from a measured zero,
  bounded history, and a structured export that doesn't require standing up an
  observability stack.
- **Release receipt.** Deliberately not built — the packaged-artifact E2E and
  MLX CI runs are the evidence trail; nothing today consumes a separate
  machine-readable receipt format.
- **Hardware detection and explainable model selection.** Partly built — this
  entry previously overstated what is left. The neutral architecture is
  delivered: `backends.HostResources` (`backends/types.go`) carries both the
  discrete-VRAM and unified-memory axes in one type; `acceleratorProbe`
  (`bootstrap/service.go`) asks each registered backend for its own probe with
  no branching on backend names; and fit policy is already per-backend
  (`backends/llamacpp/fit.go` discrete, `backends/mlx/fit.go` unified). Live
  probes exist for NVIDIA (`nvidia-smi`, `backends/llamacpp/host.go`) and Apple
  silicon (`backends/mlx/host.go`).

  What is actually left:
  - AMD probe. No coverage today.
  - CPU-only as an explicit named state rather than an empty accelerator slice,
    so the UI can say "CPU only" instead of showing nothing.
  - Multi-GPU capacity policy — see the bug in the session notes.
  - Context- and quantization-aware sizing. `core/modelcatalog/fit.go` models
    memory as `on-disk size + DefaultOverheadBytes` (a flat 512 MiB) and
    ignores context length entirely. KV cache scales with context length and
    dominates that constant at long contexts, so a model can currently be
    reported as fitting and then fail to load.
  - Explanations carried through to the user. `Verdict.Reason` is the existing
    seam.
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
| Model-serving observability | Medium | High | Few instrumentation points (load, first token, stream end, queue in/out) but all on the hot path and per-backend. Ring buffer + JSON export is easy. The fiddly part is propagating "not measured" (distinct from zero) out to web UI and TUI. |
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

### Suspected bug: multi-GPU VRAM is summed

`probeNVIDIAVRAM` (`backends/llamacpp/host.go`) sums `memory.total` across all
NVIDIA GPUs into a single figure. Two 12 GB cards therefore report 24 GB, and a
20 GB model is reported as fitting when it will not load unless the backend
splits layers across devices.

Summing is right for capacity accounting and wrong for a single-model fit
verdict. Not yet confirmed against real multi-GPU hardware — verify before
treating it as a defect. Independent of any new feature work.

### Open questions

1. **Multi-GPU policy.** Should fit use the largest single device, the sum, or
   a backend-declared "can this backend split layers" capability? Blocks the
   bug above.
2. **Is GGUF metadata already parsed anywhere?** KV-cache-aware sizing needs
   layer count, KV head count and head dimension. If a parser exists, the
   sizing work is cheap; if not, it needs a new one and the estimate grows.
   Not yet checked.

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
