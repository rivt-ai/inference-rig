# 19 — Prune the planning docs and merge to main

Type: task
Status: partially resolved (docs done; merge/QA remain)
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

## Answer (partial — everything except the QA-gated merge)

`phase-01-bootstrap` was already squash-merged to `main` as PR #1 well before
this ticket was picked up, and feature work continued directly on `main` via
ordinary PRs from then on — so "merge `phase-01-bootstrap`" no longer applies
literally; there is no divergent branch left to merge.

`docs/system-coverage-and-e2e-plan.md` was already gone by the time this
ticket was worked (removed earlier, untracked here).

Done in the PR that also closed tickets 14/15/16/17:

- `docs/feature-roadmap.md` rewritten to list only undone work plus Milestone D.
- `docs/hardware-validation.md`'s artifact/evidence table brought current (four
  targets, all published, gated by MLX + packaged E2E; no separate receipt —
  that was cut, see `map.md`).
- `README.md` gained an `## Install` section (curl | sh, inspect-first,
  stable/dev, SHA256SUMS/SBOM/attestation verification, macOS unsigned-binary
  caveat) and dropped its stale "no release published yet" line.
- `docs/agents/claiming-tickets.md` deleted, along with its `AGENTS.md` pointer.
- Gateway security, backend concurrency and release identity/channels decisions
  folded into `CONTEXT.md`. `docs/adr/` does not exist and was not created —
  `CONTEXT.md` was judged sufficient for what needed to survive.
- Resolved ticket files (01–10, 12–17, 20, 21) deleted from `.scratch/`; 11, 18
  and this file were kept because the QA gates they cover have not run yet.

Not done, deliberately: `docs/architecture-overview.md`, `docs/porting-matrix.md`
and `docs/models-ini-two-way-sync.md` were not re-audited against the tree —
nothing in this effort's work touched what they describe, so reconciling them
is out of scope here rather than skipped.

Remaining before this ticket fully closes: run ticket 18 (which needs 11
first), then re-check this ticket's acceptance criteria hold and delete
`.scratch/release-readiness/` entirely.
