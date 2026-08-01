# 05 — Runtime recovery and reconciliation

Type: task
Status: resolved
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

## Answer

Resolved 2026-08-01.

`core/runtime.Supervisor.Recover` now reads its existing PID file and adopts a
survivor only after verifying that the PID is alive, its executable is the same
file as the launch spec executable, it leads its process group, it owns the
expected listening port, and its TCP/HTTP readiness probe succeeds. Adoption
sets the existing supervisor PID/PGID fields without spawning a process, so
status, stop escalation and daemon shutdown reuse the normal lifecycle path.

The classifications are defined once as `runtime.RecoveryClassification`:
`stale_pid_file`, `mismatched_executable`, `occupied_port`,
`unhealthy_survivor`, and `valid_adoptee`. Stale files are removed. A live
process that cannot be safely adopted leaves an `orphaned` slot, blocking new
lifecycle calls until the operator uses the existing reset escape hatch; a
mismatched executable is never signalled because it is rejected before the
supervisor records the PID.

`control.Manager.RecoverRuntimes` derives neutral launch specs from the stored
profiles at startup, groups profiles that share one supervisor PID file, enters
ticket 04's `reconciling` state, and rebuilds the one runtime slot. Router
profiles sharing the recovered process address are restored together. Bootstrap
runs reconciliation before opening the control server.

Recovery emits `runtime.transition` plus `runtime.recover` records carrying the
classification through the shared audit/event path. The protobuf event field is
therefore visible from CLI event commands and the public HTTP bridge; TUI and
web event views render it explicitly. This is the vocabulary the future
`doctor` command should consume rather than reclassifying host state.

Coverage includes real-process supervisor tests for every classification,
manager adoption without `Start`, and a compiled-binary llama.cpp E2E in the
existing harness: SIGKILL the daemon, prove the engine PID survives, restart,
observe the same adopted PID and `valid_adoptee`, then prove graceful daemon
shutdown stops it. `make e2e`, `make test`, and `make lint` pass.
