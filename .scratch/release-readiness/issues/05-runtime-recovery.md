# 05 — Runtime recovery and reconciliation

Type: task
Status: claimed
Blocked by: 04
Milestone: A
Roadmap: P0 #1

## Question

Make a control-daemon restart reconcile with surviving engine processes instead
of forgetting them.

Runtime ownership lives only in the manager's in-memory map, so a daemon
restart orphans every running engine.

Do:

- Add `Recover(context.Context)` to the neutral runtime contract
  (`core/runtime/`), engine-agnostic.
- Read and validate PID files at daemon startup (`platform/pidfile`).
- Before adopting a survivor, verify executable identity, process group,
  expected port and readiness endpoint.
- Rebuild runtime slots from what was reconciled, entering the `reconciling`
  state from ticket 04.
- Classify separately: stale PID file, mismatched executable, occupied port,
  unhealthy survivor, valid adoptee. These classifications are the vocabulary
  the future `doctor` will report — name them once, in one place.
- Expose results through events, audit log, CLI, TUI and web UI.

Acceptance:

- Restarting the daemon does not orphan a managed engine.
- A valid survivor is adopted **without** being restarted.
- A stale or mismatched PID is never adopted.
- `status`, `stop` and daemon shutdown all work after adoption.
- Crash-and-restart E2E coverage using compiled binaries, in `test/e2e/`
  alongside the existing harness (`test/e2e/harness_test.go`) — reuse it, do
  not build a second one.
- `make test` and `make lint` green.
