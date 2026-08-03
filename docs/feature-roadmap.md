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
- **Hardware detection and explainable model selection.** Deterministic
  hardware envelopes, discrete-VRAM vs. unified-memory policy, and
  recommendations tied to actual artifact sizes and context requirements.
  Depends on each backend contributing probes and fit policy without shared
  code branching on concrete backend names.
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
