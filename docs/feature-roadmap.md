# InferenceRig Alpha: Core Roadmap

Date: 2026-07-31  
Reviewed branch: `phase-01-bootstrap`

This roadmap covers the InferenceRig control plane, inference backends, operations, packaging, and selected feature ideas worth adding.

## Prioritized fixes

### P0 — required before beta

#### 1. Fail fast on invalid configuration — partially done

Daemon and web startup currently fall back to defaults for every configuration-loading error. Existing malformed or unreadable configuration must not silently change security settings, paths, or listen addresses.

`inferencerig config validate` exists, and there is test coverage around autostart validation, but whether every load path (daemon, web) actually hard-fails on syntax/permission/I-O errors rather than falling back to defaults has not been confirmed against `config/config.go`.

Required work:

- Confirm (or fix) that defaults are used only when the config file does not exist.
- Treat syntax, validation, permission, and I/O failures as startup errors.
- Print the resolved path and exact failing field.
- Cover malformed YAML, unknown keys, unreadable files, and invalid security combinations.

#### 2. Gate releases on product-level tests — partially done

CI (`.github/workflows/`) already runs lint, unit/integration tests, real llama.cpp E2E, and an MLX workflow. Not yet confirmed: browser E2E gating, multi-platform artifact publishing, and the release-evidence pipeline below.

Required work:

- Require browser E2E in the release gate.
- Publish at least Linux amd64, Linux arm64, and macOS arm64 artifacts.
- Test setup, daemon startup, RPC health, gateway health, profile lifecycle, inference, and shutdown against packaged artifacts.
- Generate checksums, SBOMs, provenance, and signed artifacts.
- Attach a release receipt recording platform, engine, model, revision, and results.

### P2 — consistency and product polish

#### 3. Correct documentation and evidence

- Remove obsolete `make e2e-live` instructions.
- Record actual hardware validation runs.
- Distinguish contract, control-stack, simulated, and hardware evidence.
- Update PR #1's stale bootstrap-only description.
- Document platform support separately from released artifact availability.

#### 4. Improve observability — partially done

Operation IDs, events, and audit logging exist (`core/control/events.go`, `audit_sink.go`, `journal.go`). Model-serving metrics (load time, time to first token, throughput, queue depth) were not found.

Required work:

- Report model-load time, time to first token, throughput, queue depth, request count, and failures where supported.
- Preserve “not measured” separately from a measured zero.
- Retain bounded server-side metrics history.
- Add structured export suitable for troubleshooting without requiring a large observability stack.

## Additional features worth adding

These are adopted as designs or feature ideas, not as copied code.

### 1. Hardware detection and explainable model selection

Adopt:

- deterministic hardware envelopes;
- discrete-VRAM and unified-memory policies;
- recommendations tied to actual artifact sizes and context requirements;
- explicit explanations for selection and rejection;
- post-launch fit and performance verification.

Each backend should contribute probes and fit policy. Shared code should rank compatible model variants without branching on concrete backend names.

### 2. Layered validation and release receipts

Adopt the separation between:

- inexpensive CI;
- clean-machine bootstrap;
- real-engine inference;
- browser/control workflows;
- hardware validation;
- lifecycle recovery;
- final release evidence.

Every release should state exactly which engine, model, platform, architecture, and lifecycle flows passed.

### 3. Model switchboard validation

After switching models, prove more than port readiness:

- the endpoint returns the selected model identity;
- token generation succeeds;
- streaming and cancellation work;
- the previous model is unloaded or intentionally retained;
- resource accounting updates correctly;
- rollback works.

### 4. Installer trust and immutable pinning

Adopt:

- stable versus development channels;
- installation from a tag or audited commit;
- inspect-first instructions;
- immutable dependency and model revisions;
- checksums, provenance, and reproducible release receipts.

### 5. Lifecycle diagnostics

Reinstall, restart, recovery, and doctor flows are directly relevant here and more valuable to InferenceRig now than adding another inference backend.

### 6. Hardware-aware defaults

Provide recommended defaults for:

- model variant and quantization;
- context length;
- batch and parallelism settings;
- GPU layer/offload policy;
- memory headroom;
- readiness timeout.

Defaults must remain visible, explainable, and overrideable in canonical profiles.

### 7. Backup, export, and migration

Add versioned export/import for:

- canonical profiles;
- global configuration;
- model inventory metadata;
- engine installation records;
- audit and validation receipts.

Do not include model bytes unless explicitly requested.

### 8. Support matrix with evidence levels

Track separately:

- code-supported platforms;
- CI-tested platforms;
- hardware-validated platforms;
- released binary targets;
- experimental accelerator paths.

Avoid presenting interface implementation or contract tests as hardware proof.

## Features to keep outside the core

InferenceRig should remain focused. The following do not belong in its core control plane:

- RAG databases and ingestion pipelines;
- autonomous-agent frameworks;
- general workflow automation;
- speech and image-generation stacks;
- full homelab orchestration;
- general-purpose observability suites.

External systems may consume stable inference endpoints without becoming responsibilities of the core.

## Suggested delivery sequence

### Milestone A — beta reliability

1. Fail-fast configuration — confirm/close remaining gap (item 1 above).

### Milestone B — release readiness

1. Packaged-artifact E2E — confirm browser E2E is in the gate.
2. Multi-platform builds.
3. Signed artifacts, SBOMs, and provenance.
4. Release receipts and evidence-based support matrix.

### Milestone C — operational quality

1. Upgrade and rollback.
2. Backup/export and migration.
3. Hardware-aware model recommendations.
4. Metrics history and diagnostic bundles.

### Milestone D — extensibility

1. Stabilize backend capability and concurrency contracts.
2. Publish a backend authoring guide and contract-test kit.
3. Add compatibility/version policy for external backends.
4. Consider additional engines only after recovery and release gates are proven.

## Final recommendation

Recovery, autostart, gateway security, per-backend state machines, download persistence, install/rollback hardening, and `doctor` have since shipped. What remains: confirming fail-fast config behavior end-to-end, closing the release-evidence gap (SBOMs, signing, browser E2E in the gate), documentation/evidence cleanup, and model-serving observability (TTFT/throughput metrics).
