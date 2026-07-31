# 04 — Explicit runtime state machine with non-blocking transitions

Type: task
Status: resolved
Blocked by: 03
Milestone: A
Roadmap: P1 #8 + P1 #6

## Question

Replace inferred runtime state with an explicit state machine, and stop holding
the manager mutex across blocking work.

These are one change, not two: both rewrite how `core/control` owns a runtime
slot, and doing them separately means the second agent redesigns the first's
work.

**Read ticket 03's `## Answer` first — it is the spec for the slot model.** In
short: one active backend globally, at most one `runtimeSlot{process, profiles}`
keyed by backend, exclusive versus router behaviour derived from
`SingleActiveProfile` + `RuntimeActivator`, no implicit kills.

States: `stopped`, `reconciling`, `starting`, `activating`, `running`,
`stopping`, `failed`, `orphaned`.

Proto changes this ticket owns (run `make generate`): a `replace` field on
`StartRuntimeRequest`, `active_backend` on the info message, and one new reset
RPC that stops every runtime and clears the active backend. Plus the registry
guard from ticket 03 §1.

Do:

- Give each slot (keyed per ticket 03's answer) a state machine; state is
  never inferred from map membership or a process flag.
- Every transition records timestamp, operation ID, profile, backend and a
  typed result.
- Reserve the transition under the lock, do the blocking work (stop, spawn,
  readiness probe) **outside** it, then commit the result under the lock again.
  A cold engine start must not block `status` or an unrelated backend's
  lifecycle call.
- Define deterministic behaviour for concurrent start / stop / restart / delete
  on the same slot — losers get a typed error, not a queue.
- Stream progress as events rather than making status calls wait.

Acceptance:

- A test starts a slow engine and asserts `status` for another profile returns
  promptly while it is starting.
- Concurrent start+stop on one slot resolves deterministically, both orderings
  covered by tests.
- Every transition is observable in the event stream and audit log.
- `make test` and `make lint` green.

Lazy note: the operation ID introduced here is what the map's fogged
observability work will later reuse — emit it, do not build a metrics system
around it now.

## Answer

Resolved 2026-07-31. Ticket 03's answer implemented as written.

### Where it lives

`core/control/slot.go` is new and owns the whole model: `runtimeSlot`, the
`operation` record, and the reserve/commit pair each lifecycle call runs
through. `core/control/manager.go` keeps the public methods and now holds `mu`
only to reserve or commit — never across a stop, spawn, readiness probe or
activation.

- **One slot, one active backend.** `Manager.runtimes` (a map keyed by backend)
  is gone, replaced by a single `*runtimeSlot`. The slot existing *is* the
  active backend, so "set when the first profile starts, cleared when the last
  stops" needed no second field to drift out of step.
- **State is explicit.** `coreruntime.State` gained `Reconciling`,
  `Activating` and `Orphaned` alongside the five it had, and the slot carries
  one. Nothing reads state off map membership or a process pointer. The wire
  field was already a string, so no client breaks on the new values.
- **Transitions.** `AuditEvent` and `Event` gained `OperationID`, `Profile`,
  `Backend` and `State`, empty on every non-transition action. They go through
  the existing `MultiAuditSink`, so one emit reaches the event stream, the
  `WatchEvents` RPC and the slog audit log at once. Operation IDs are `op-N`
  from a counter under `mu`.
- **Concurrency.** A lifecycle call reserves the slot or gets `ErrorConflict`
  immediately — never a queue. Only a `running` slot accepts a start or stop,
  which covers `failed` and (for ticket 05) `orphaned` with no extra branches.
  Reset additionally accepts any settled state, since it is the escape hatch.
- **Failure policy.** A failed start drops the slot: nothing is running and
  holding the active backend would block a backend that could have started. A
  failed stop keeps it in `failed`, because a process may have survived, and
  every later call conflicts until a reset.

### Contract and wire

- `Registry.Register` rejects `SingleActiveProfile: false` without
  `RuntimeActivator` (ticket 03 §1). `backendtest.Fake` therefore declares
  `SingleActiveProfile: true`; tests that need a router embed it and override.
- Proto: `replace` on `StartRuntimeRequest`, `active_backend` on
  `GetInfoResponse`, `ResetRuntimes`, and the four transition fields on `Event`.
  CLI gained `runtime start --replace` and `runtime reset`; MCP gained the
  `replace` argument and a `runtime_reset` tool; the gateway's mutating-procedure
  set gained `ResetRuntimes`.

### One thing ticket 03 did not anticipate

A router process binds the listen address of **whichever profile started it**
(`backends/llamacpp/launch.go` uses `p.Listen.Host/Port`), and `ActivateRuntime`
posts to the *activated* profile's address. So a second router profile can only
join the running process when it names the same address; otherwise activation
would go to an address nothing listens on and the manager would report a runtime
that serves nobody. The slot records its address and a profile listening
elsewhere needs `replace`, exactly like an exclusive backend. Covered by
`TestRouterProfileOnAnotherAddressNeedsTheProcessReplaced`.

### Not done here — ticket 03 §5's front ends

`Info.ActiveBackend` is exposed and every front end can read it, but the TUI and
web UI still render profiles the same way and pass no `replace`. Until that
lands, starting a second profile from either UI surfaces the typed conflict as a
plain error with no inline reset or confirm. This is a UI change across the
bubbletea dashboard and the Svelte panels, outside this ticket's Do and
Acceptance lists; it must land before **ticket 11** (Milestone A manual QA) can
pass. Recorded on the map.

### For later tickets

- **05 (recovery)** sets `Reconciling` and `Orphaned` on the slot; both states
  already conflict with start and stop and are cleared by reset, so
  reconciliation only has to classify.
- **06 (autostart)** reads `Info.ActiveBackend` and must reject a mixed-backend
  autostart set at config validation.
- **Observability** reuses `Event.OperationID`; no metrics system was built
  around it.
