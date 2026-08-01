# 21 — Make TUI profile creation and restart reachable

Type: task
Status: claimed
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
