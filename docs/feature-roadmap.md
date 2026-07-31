# InferenceRig Alpha: Core Roadmap

Date: 2026-07-31  
Reviewed branch: `phase-01-bootstrap`

This roadmap covers the InferenceRig control plane, inference backends, operations, packaging, and selected ideas worth adopting from ODS.

## Prioritized fixes

### P0 — required before beta

#### 1. Implement runtime recovery and reconciliation

Current runtime ownership exists only in the control manager's in-memory map. A daemon restart therefore loses knowledge of surviving engine processes.

Required work:

- Add `Recover(context.Context)` to the neutral runtime contract.
- Read and validate PID files during daemon startup.
- Verify executable identity, process group, expected port, and readiness endpoint before adoption.
- Rebuild runtime slots from reconciled processes.
- Classify stale PID files, mismatched executables, occupied ports, unhealthy survivors, and valid adoptees separately.
- Expose reconciliation results through events, audit logs, CLI, TUI, and web UI.
- Add crash-and-restart E2E coverage using compiled binaries.

Acceptance criteria:

- Restarting the control daemon does not orphan a managed engine.
- A valid survivor is adopted without being restarted.
- A stale or mismatched PID cannot be adopted.
- `status`, `stop`, and daemon shutdown work after adoption.

#### 2. Make profile autostart operational

Autostart profile names are persisted and displayed, but daemon bootstrap does not start or reconcile them.

Required work:

- Reconcile existing processes before starting configured profiles.
- Start autostart profiles in deterministic order.
- Define conflicts when several profiles target a single-active backend.
- Reject or explain impossible combinations during validation.
- Add bounded retries and backoff without creating infinite crash loops.
- Report partial startup clearly.
- Add optional systemd and macOS LaunchAgent integration.

#### 3. Fail fast on invalid configuration

Daemon and web startup currently fall back to defaults for every configuration-loading error. Existing malformed or unreadable configuration must not silently change security settings, paths, or listen addresses.

Required work:

- Use defaults only when the config file does not exist.
- Treat syntax, validation, permission, and I/O failures as startup errors.
- Print the resolved path and exact failing field.
- Add `inferencerig config validate`.
- Cover malformed YAML, unknown keys, unreadable files, and invalid security combinations.

#### 4. Gate releases on product-level tests

Required work:

- Require unit/integration tests, lint, llama.cpp E2E, and browser E2E.
- Require MLX hardware validation for releases claiming MLX support.
- Publish at least Linux amd64, Linux arm64, and macOS arm64 artifacts.
- Test setup, daemon startup, RPC health, gateway health, profile lifecycle, inference, and shutdown against packaged artifacts.
- Generate checksums, SBOMs, provenance, and signed artifacts.
- Attach a release receipt recording platform, engine, model, revision, and results.

#### 5. Define and enforce the gateway security model

Mutations require a bearer token, but reads can expose profiles, installed models, runtime state, telemetry, logs, and audit information.

Required work:

- Authenticate all control RPCs by default, or introduce explicit read/manage scopes.
- Make anonymous reads opt-in rather than implicit.
- Reject unauthenticated non-loopback binds by default.
- Separate control credentials from inference credentials.
- Redact secrets, tokens, sensitive paths, and engine arguments from logs and APIs.
- Add tested reverse-proxy and trusted-origin configuration.

### P1 — operational hardening

#### 6. Remove blocking work from the global runtime lock

Runtime start and stop currently hold the manager mutex while processes stop, launch, and wait for readiness. A cold engine start can block status or lifecycle operations for unrelated backends.

Required work:

- Introduce per-backend or per-slot state machines.
- Reserve a transition under lock, perform blocking work outside it, then commit the result.
- Define deterministic behavior for concurrent start, stop, restart, and delete operations.
- Stream progress events instead of blocking status calls.

#### 7. Persist downloads and recover partial artifacts

Required work:

- Persist job metadata atomically.
- Reconcile `.part` files at startup.
- Resume only when safe range requests are supported.
- Verify SHA-256 or stronger digests from catalog metadata.
- Resolve mutable repositories to immutable revisions before download.
- Enforce maximum size, redirect, hostname, and protocol policies.

#### 8. Introduce an explicit runtime state machine

Use defined states rather than inferring state from map membership and process flags:

- stopped
- reconciling
- starting
- activating
- running
- stopping
- failed
- orphaned

Every transition should record a timestamp, operation ID, profile, backend, and typed result.

#### 9. Harden engine installation and rollback

- Preserve llama.cpp digest and size validation.
- Validate every archive entry before extraction to prevent path traversal.
- Pin MLX packages with hashes or a locked artifact set.
- Record engine source, version, digest, platform, accelerator, and installation time.
- Verify staged binaries before activation.
- Make rollback a supported operation instead of merely retaining prior files.

#### 10. Add an operator-grade `doctor`

Checks should include:

- config validity and permissions;
- socket and PID ownership;
- stale or orphaned processes;
- engine versions and installation integrity;
- port conflicts;
- model paths, hashes, sizes, and free space;
- accelerator discovery;
- gateway bind and authentication posture;
- runtime logs and recent failures;
- supported remediation commands.

Support human-readable and JSON output.

### P2 — consistency and product polish

#### 11. Correct documentation and evidence

- Remove obsolete `make e2e-live` instructions.
- Record actual hardware validation runs.
- Distinguish contract, control-stack, simulated, and hardware evidence.
- Update PR #1's stale bootstrap-only description.
- Document platform support separately from released artifact availability.

#### 12. Improve observability

- Add stable operation IDs across control, gateway, and engine logs.
- Report model-load time, time to first token, throughput, queue depth, request count, and failures where supported.
- Preserve “not measured” separately from a measured zero.
- Retain bounded server-side metrics history.
- Add structured export suitable for troubleshooting without requiring a large observability stack.

#### 13. Clarify backend concurrency semantics

The contract exposes backend capabilities, while the manager currently maintains one runtime slot per backend. Explicitly model whether a backend supports:

- one process and one active profile;
- one router process with several loaded profiles;
- several independent processes;
- multi-model routing inside one process.

Runtime slots, validation, autostart, and UI behavior should derive from the declared concurrency policy.

## Features worth adopting from ODS

“Cherry-pick” should usually mean adopting the feature or design rather than copying commits directly. ODS and InferenceRig use different architectures. ODS is Apache-2.0 and InferenceRig is MIT; copied Apache-licensed code must retain the required license and attribution notices.

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

ODS's focus on reinstall, restart, recovery, and doctor flows is directly relevant. These paths are more valuable to InferenceRig now than adding another inference backend.

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

1. Fail-fast configuration.
2. Runtime recovery and reconciliation.
3. Real profile autostart.
4. Per-backend lifecycle state machines.
5. Gateway security policy.
6. Persistent download state and integrity checks.

### Milestone B — release readiness

1. Packaged-artifact E2E.
2. Multi-platform builds.
3. Required llama.cpp and MLX hardware gates.
4. Signed artifacts, SBOMs, and provenance.
5. Release receipts and evidence-based support matrix.

### Milestone C — operational quality

1. `inferencerig doctor`.
2. Upgrade and rollback.
3. Backup/export and migration.
4. Hardware-aware model recommendations.
5. Metrics history and diagnostic bundles.

### Milestone D — extensibility

1. Stabilize backend capability and concurrency contracts.
2. Publish a backend authoring guide and contract-test kit.
3. Add compatibility/version policy for external backends.
4. Consider additional engines only after recovery and release gates are proven.

## Final recommendation

The next InferenceRig release should prioritize recovery, truthful configuration behavior, secure gateway defaults, real autostart, and release evidence. Those changes convert the current strong alpha architecture into an operationally dependable inference control plane.
