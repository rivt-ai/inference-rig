# Map: Release and production readiness

Effort: `release-readiness` · Started 2026-07-31

## Status

Milestone B is implemented. Tickets 01–10, 12–17, 20, 21 are resolved and their
decisions are folded into [`CONTEXT.md`](../../CONTEXT.md) (gateway security,
backend concurrency, release identity/channels) and
[`docs/hardware-validation.md`](../../docs/hardware-validation.md) (evidence
levels, the current artifact/evidence matrix). `v0.1.0` shipped before the SBOM
and provenance work landed, from a repo that had already had
`phase-01-bootstrap` squash-merged to `main` as PR #1 — the ticket branch
workflow this map originally described was superseded by ordinary PRs partway
through, which is why this file no longer tracks per-ticket branch mechanics.

What is left is exactly two manual QA gates, both HITL:

- [11 — Milestone A manual QA](issues/11-manual-qa-milestone-a.md) — all
  blockers resolved, ready to run.
- [18 — Milestone B manual QA](issues/18-manual-qa-milestone-b.md) — blocked by
  11.

Ticket 19 (prune docs, dispose of this directory) has had its non-QA content
done as part of the same PR that closed 14/15/16/17: `docs/feature-roadmap.md`
pruned to only undone work, `docs/hardware-validation.md` and `README.md`
brought current, `docs/agents/claiming-tickets.md` removed, decisions folded
into `CONTEXT.md`, resolved ticket files deleted. What remains of 19 is
recorded there directly rather than here.

## Not yet specified

Milestone C (operational quality) is in scope for a future effort but not yet
sharp — graduate these into tickets once ticket 18 is signed off, per its own
note.

- **`inferencerig doctor`** (roadmap) — check list depends on the runtime
  states, reconciliation classifications and config validation errors that
  now exist to report on.
- **Upgrade and rollback** — shape depends on the install script and release
  channels, which now exist (`internal/installer/install.sh`, `stable`/`dev`).
- **Backup, export and migration** — versioned export of profiles, config,
  model inventory, engine install records and receipts.
- **Hardware-aware defaults and explainable model fit** — split between
  `backends` fit policy and `core/modelcatalog`.
- **Observability** — operation IDs across control/gateway/engine logs,
  bounded metrics history, "not measured" versus measured zero, diagnostic
  bundle.
- **Model switchboard validation** — proving model identity, streaming,
  cancellation and unload after a switch.

## Out of scope

- RAG databases and ingestion pipelines; autonomous-agent frameworks; general
  workflow automation; speech and image generation; homelab orchestration;
  general-purpose observability suites.
- Windows support — the canonical local transport is a Unix socket.
- Milestone D (extensibility): backend authoring guide, external-backend
  compatibility policy, additional engines. A later effort.
- Package managers beyond the install script (Homebrew tap, distro packages).
