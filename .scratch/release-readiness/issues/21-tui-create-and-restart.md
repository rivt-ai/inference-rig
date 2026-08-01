# 21 — Make TUI profile creation and restart reachable

Type: task
Status: resolved
Blocked by: 20
Milestone: A
Roadmap: quality gate prerequisite

## Question

Ticket 11's manual TUI pass requires profile create and restart actions, but
neither action is currently reachable from the TUI.

Do:

- Let the TUI create a valid neutral profile through the existing `PutProfile`
  RPC, without backend-specific terms.
- Make restart reachable from normal profile selection, confirm it as a
  destructive action, and clear the confirmation state after dispatch through
  the existing `RestartRuntime` RPC.
- Add focused tests at the TUI interaction and dispatch seams.

Acceptance:

- The TUI can create a valid neutral profile through existing
  `PutProfile`/RPC behavior, with required fields and validation errors visible.
- A reachable restart action uses existing `RestartRuntime` with confirmation
  and clear state.
- Focused TUI tests pass.
- `make test` and `make lint` pass.

## Answer

Resolved 2026-08-01. On the Models page, `n` on a selected downloaded local
model now creates a backend-neutral structured profile through the existing
`PutProfile` RPC. The TUI supplies every required common field: a safe stable
name derived from the model path, the selected backend, the local model path,
loopback host, and the next unused port from 8080. Canonical and backend
validation remain server-owned, and failures appear through the dashboard's
existing action warning.

`R` on the selected running profile uses the existing press-again confirmation
and dispatches `RestartRuntime`. Restart is explicit on the service request, so
the second runtime row's existing action index still means stop; the
confirmation is clear once dispatched. The Models page now reuses one status
owner for create/start/restart/cleanup results instead of carrying parallel
confirmation state, keeping the module under the enforced Go LOC budget.

Focused `go test ./adapters/tui`, `make test`, and `make lint` pass.

Review follow-up: local-model snapshots now carry the backend that produced
them, so a backend switch cannot create from stale rows while its refresh is in
flight. Port allocation accepts 65535 as the last valid choice and reports
exhaustion through the existing visible action-warning path instead of reusing
8080.
