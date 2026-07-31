# Map: Release and production readiness

Effort: `release-readiness` · Started 2026-07-31 · Integration branch `phase-01-bootstrap`

## Destination

A signed, multi-platform, publicly installable InferenceRig release — Milestones
A (beta reliability), B (release readiness) and C (operational quality) from
`docs/feature-roadmap.md` — where the release states with evidence which
platform, engine and model it was proved on.

Reached when a tagged GitHub Release ships Linux amd64, Linux arm64 and macOS
arm64 artifacts with checksums, SBOM, provenance and an install script, backed
by green llama.cpp E2E, browser E2E, an MLX CI job that really infers, and
signed-off manual TUI + web UI passes.

## Notes

Domain: a neutral Go control plane for local inference engines. Required
reading before code: `AGENTS.md` (neutrality + quality rules) and
`docs/architecture-overview.md` (layers, import direction, entry points).
`docs/feature-roadmap.md` is where these work items come from;
`docs/system-coverage-and-e2e-plan.md` describes the test layers — most of that
plan already landed (`scripts/go-coverage.sh`, `make e2e`, `make e2e-browser`,
`make e2e-live-mlx`, `test/e2e/`), so verify before rebuilding any of it.

**Every session must:**

- Invoke `/ponytail` first and keep the change the laziest one that works.
  Simplicity is the standing instruction for this effort — prefer stdlib and
  existing owners over new packages, one line over fifty.
- Claim the ticket before any work: set `Status: claimed` in the ticket file
  and save it.
- Work on a branch `ticket/NN-<slug>` cut from `phase-01-bootstrap`, and merge
  back into `phase-01-bootstrap`. `main` stays untouched until the release.
- Resolve exactly **one** ticket per session (research tickets excepted).
- Commit small and often; leave `make test` and `make lint` green before
  pushing. Never weaken or skip a test to make the gate green.
- Record the outcome under `## Answer` in the ticket, set `Status: resolved`,
  then append one line to **Decisions so far** below.

**Standing facts for this effort:**

- No users exist yet — breaking changes to config, profile schema, CLI flags
  and gateway defaults are allowed with no migration path. Note them in the
  release notes instead.
- MLX is verified by the GitHub `macos-15` CI job throughout; local Apple
  Silicon is reserved for the final release validation only.
- Manual QA at each milestone gate = agent automated dry-run first, then a
  trimmed numbered script the human runs on TUI and web UI against a real
  llama.cpp profile.

Skills: `/ponytail` always; `/grilling` + `/domain-modeling` on grilling
tickets; `/tdd` where the behaviour is testable; `/research` on research
tickets.

**Picking a ticket:** the frontier is every file in `issues/` whose `Status:` is
`open` and whose `Blocked by:` tickets are all `Status: resolved`. Lowest number
wins. Grilling tickets need the effort owner present; task and research tickets
do not.

## Decisions so far

<!-- one line per resolved ticket: gist + link -->

- Charting session (2026-07-31) — Destination is Milestones A+B+C; the map
  carries execution as well as decisions; release vehicle is GitHub Releases
  plus a stable/dev install script over Linux amd64, Linux arm64 and macOS
  arm64; manual QA is an agent dry-run followed by a human-run script; MLX via
  `macos-15` CI until the final local-hardware validation; agents branch per
  ticket off `phase-01-bootstrap`; no existing users, so no migrations.
- [01 — Decide the gateway security model](issues/01-gateway-security-policy.md)
  — Authenticate every RPC and `/mcp` (delete `mutatingProcedures`), excepting
  `/health` and the static app shell; no login system — the token persists to
  `run/gateway.token` and is delivered by a `#token=` launch URL; insecure mode
  stays but a non-loopback bind additionally requires
  `allow_exposed_without_auth`; posture shown in startup log, `/health`, TUI
  badge and a non-loopback-only web banner; redact credentials only, not paths
  or argv; `AllowedOrigin` becomes a list, no `X-Forwarded-*` trust.
- [03 — Decide backend concurrency semantics](issues/03-backend-concurrency-semantics.md)
  — No new contract surface: `SingleActiveProfile` + `RuntimeActivator` already
  distinguish exclusive (MLX) from router (llama.cpp) backends, and the manager
  simply starts honouring them. One backend active at a time globally; the
  active backend is tracked, cross-backend starts conflict, and an explicit
  reset (not a daemon restart) switches. At most one
  `runtimeSlot{process, profiles}` keyed by backend. No client can kill a
  running engine implicitly — conflict unless `replace: true`. Non-active
  backend profiles stay listed but render unstartable with the reason.

## Not yet specified

Milestone C (operational quality) is in scope but not yet sharp — each item
below hangs on what Milestone A's state machine, reconciliation and config
behaviour actually expose. Graduate these into tickets once
`11-manual-qa-milestone-a` is signed off.

- **`inferencerig doctor`** (roadmap #10) — the check list is only writable
  once the runtime states, reconciliation classifications and config validation
  errors from tickets 02/04/05 exist to be reported on. Human and JSON output.
- **Upgrade and rollback** — shape depends on what the install script and
  release channels (tickets 14/15) end up producing.
- **Backup, export and migration** (roadmap ODS #7) — versioned export of
  profiles, config, model inventory, engine install records and receipts.
  Blocked on the final config/profile schema after ticket 02.
- **Hardware-aware defaults and explainable model fit** (roadmap ODS #1, #6) —
  how much of this belongs in `backends` fit policy versus `core/modelcatalog`
  depends on ticket 03's concurrency answer.
- **Observability** (roadmap #12) — operation IDs across control/gateway/engine
  logs, bounded metrics history, "not measured" versus measured zero,
  diagnostic bundle. Depends on the state-machine transition records from
  ticket 04.
- **Model switchboard validation** (roadmap ODS #3) — proving model identity,
  streaming, cancellation and unload after a switch. Needs ticket 03's answer
  about whether a backend routes or restarts.

## Out of scope

- RAG databases and ingestion pipelines; autonomous-agent frameworks; general
  workflow automation; speech and image generation; homelab orchestration;
  general-purpose observability suites — `docs/feature-roadmap.md` rules these
  outside the core control plane.
- Windows support — the canonical local transport is a Unix socket
  (`docs/hardware-validation.md`).
- Milestone D (extensibility): backend authoring guide, external-backend
  compatibility policy, additional engines. Past this destination; a later
  effort.
- Package managers beyond the install script (Homebrew tap, distro packages) —
  ruled out when the release vehicle was chosen.
