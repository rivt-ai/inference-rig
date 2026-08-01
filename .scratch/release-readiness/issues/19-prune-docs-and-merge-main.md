# 19 — Prune the planning docs and merge to main

Type: task
Status: open
Blocked by: 18
Milestone: B (re-wire to the last Milestone C ticket when C graduates)
Roadmap: P2 #11

## Question

Leave `main` describing the project that exists, not the project that was
planned. This is the **last ticket of the effort** — nothing merges to `main`
before it.

Note: `Blocked by: 18` is a placeholder. Milestone C is still fog on the map;
when it graduates into tickets, re-point this ticket's `Blocked by` at the last
of them.

Do:

- **Prune `docs/feature-roadmap.md`.** Every P0/P1/P2 item this effort
  delivered comes out — the roadmap is a plan, and a plan that has been
  executed is noise that misleads the next reader into re-doing it. What
  survives: items deliberately not done, with a line saying so, and Milestone D
  (extensibility), which is out of scope for this effort. If nothing survives,
  delete the file.
- **Retire `docs/system-coverage-and-e2e-plan.md`.** It is written as
  "proposed", and by this point it is fully implemented. Either delete it or
  reduce it to a short description of the test layers as they actually are.
  Ticket 10 will already have marked which phases landed — finish the job.
- **Reconcile every remaining doc against the tree**: `README.md`,
  `AGENTS.md`, `docs/architecture-overview.md`, `docs/hardware-validation.md`,
  `docs/porting-matrix.md`, `docs/models-ini-two-way-sync.md`. Commands quoted
  must exist; support claims must trace to a recorded run.
- **Fold the decisions into permanent docs.** The `## Answer` sections in this
  effort's tickets are the reasoning behind the gateway security model, the
  backend concurrency policy and the release identity. Anything a future
  contributor needs must live in `CONTEXT.md`, `docs/adr/` or the architecture
  overview — not in `.scratch/`, which is planning scratch and not permanent
  documentation.
- **Remove `docs/agents/claiming-tickets.md`** and its pointer in `AGENTS.md`.
  It describes how to work *this* map against `phase-01-bootstrap`; once that
  branch is merged the instructions are actively wrong. Keep it only if it is
  first rewritten to be effort-agnostic.
- **Decide what happens to `.scratch/release-readiness/`**: archive it in the
  repo or delete it once its decisions are folded in. Either is fine; leaving a
  resolved map lying around as if it were live is not.
- **Merge `phase-01-bootstrap` into `main`.**

Acceptance:

- No doc describes unimplemented work as if it were pending, and no
  implemented work is still described as proposed.
- Every command quoted in any doc exists.
- Every decision from this effort that outlives it is reachable from
  `CONTEXT.md`, `docs/adr/` or `docs/architecture-overview.md`.
- `make test`, `make lint`, `make coverage`, `make e2e`, `make e2e-browser`
  green on `main` after the merge.
