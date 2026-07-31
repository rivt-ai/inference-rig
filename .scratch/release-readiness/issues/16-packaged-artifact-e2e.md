# 16 — Packaged-artifact E2E and release receipt

Type: task
Status: open
Blocked by: 14
Milestone: B
Roadmap: P0 #4, ODS #2

## Question

Prove the thing users actually download works, and record what was proved.

Today `make e2e` builds binaries from source in the harness. This ticket runs
the **released artifact** through a clean-machine flow.

Do:

- A workflow job that takes a published (or pre-release) artifact, installs it
  with ticket 15's script on a clean runner, and exercises: `setup`, daemon
  startup, RPC health, gateway health, profile lifecycle, a real llama.cpp
  inference using the pinned fixtures, and clean shutdown.
- Reuse `test/e2e/harness_test.go` by pointing it at pre-built binaries instead
  of building them — add the seam, do not fork the harness.
- Emit a **release receipt**: a machine-readable file attached to the release
  recording platform, architecture, engine name and version, model and
  revision, InferenceRig revision, which test layers ran, and their results.
  It reads the engine install records from ticket 08.
- Fill in the evidence table ticket 10 left in `docs/hardware-validation.md`
  from the receipt, so support claims trace to a run.

Acceptance:

- The job fails if the artifact cannot install, start, infer or shut down
  cleanly.
- The receipt distinguishes "not run" from "passed" — a skipped layer can never
  read as evidence.
- A release with no receipt is not publishable.
- `make test` and `make lint` green.
