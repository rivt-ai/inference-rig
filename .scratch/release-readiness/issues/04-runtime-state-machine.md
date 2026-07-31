# 04 — Explicit runtime state machine with non-blocking transitions

Type: task
Status: open
Blocked by: 03
Milestone: A
Roadmap: P1 #8 + P1 #6

## Question

Replace inferred runtime state with an explicit state machine, and stop holding
the manager mutex across blocking work.

These are one change, not two: both rewrite how `core/control` owns a runtime
slot, and doing them separately means the second agent redesigns the first's
work.

States: `stopped`, `reconciling`, `starting`, `activating`, `running`,
`stopping`, `failed`, `orphaned`.

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
