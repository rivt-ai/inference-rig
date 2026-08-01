# 10 — Correct documentation and evidence levels

Type: task
Status: resolved
Blocked by: none
Milestone: A
Roadmap: P2 #11

## Question

Make the docs describe the repo that exists. Small, self-contained, safe to run
in parallel with anything.

Known drift as of 2026-07-31:

- `docs/hardware-validation.md` still tells the reader to run `make e2e-live`,
  a target the Makefile no longer has. The real targets are `make e2e`,
  `make e2e-browser` and `make e2e-live-mlx`.
- `docs/system-coverage-and-e2e-plan.md` is written as "proposed", but Phases
  1–4 largely landed: `scripts/go-coverage.sh`, `scripts/provision-e2e-llamacpp.sh`,
  `test/e2e/{harness,cli_lifecycle,gateway,browser,mlx_live}_test.go`,
  `.github/workflows/{e2e,mlx}.yml`, and `GO_COVERAGE_MIN = 65` in the Makefile.
  Verify what actually landed against the plan and mark each phase done or
  outstanding — do not assume either way.
- `docs/architecture-overview.md` references `test/e2e/` generically; check its
  "Working on this repo" section still lists the right commands.
- PR #1's description is stale ("bootstrap only") per the roadmap.

Do:

- Fix the above, verifying each claim against the tree rather than the prose.
- Document the four evidence levels distinctly — contract verified,
  control-stack verified, CI-tested, hardware verified — and state which
  platforms hold which today. Contract tests are not hardware proof.
- Document platform support separately from released-artifact availability.
- Leave a table shape that ticket 16's release receipt can fill in.

Acceptance: every command quoted in the docs exists in the Makefile; no doc
claims hardware validation that has not been recorded.

## Answer

Docs-only. Every claim below was checked against the tree, not against the
prose it replaced.

### `docs/hardware-validation.md` — rewritten

Was the worst offender: it told the reader to run `make e2e-live` (gone) with
`INFERENCERIG_LIVE_LLAMACPP_BIN` / `_MODEL` (variables that exist nowhere in
the repo), and said a missing fixture causes the live test to be "explicitly
skipped" — the exact opposite of `requireEnv`, which fails the run precisely so
a skip cannot masquerade as a pass.

Now documents **four** evidence levels, each with what it does and does not
prove: contract verified → control-stack verified → CI-tested → hardware
verified. Note the third level's name: the roadmap (P2 #11) calls it
"simulated", this ticket calls it "CI-tested"; the ticket's word is used,
because nothing about a real engine generating real tokens on a real runner is
simulated.

Two separate tables, because they are two separate claims:

- **Evidence matrix** — platform × engine × level. Contract and control-stack
  read "yes" on rows whose CI column reads "no", because they are portable Go
  and not an engine-support claim for that platform; the doc says so explicitly.
- **Platform support versus released artifacts** — supported is linux/amd64,
  linux/arm64, darwin/arm64; `release.yml` publishes **linux/amd64 only** today
  (verified: one `go build` in the workflow). Ticket 14 closes that gap.

That second table is the shape ticket 16's release receipt fills in: one row per
published artifact naming engine, model, runner and evidence level, so a row
with no recorded run says so instead of inheriting another platform's claim.

Hardware verified is defined as *recorded*, and "Recorded hardware runs" is
empty. No doc in the repo now claims hardware validation.

### `docs/system-coverage-and-e2e-plan.md` — status recorded, plan text kept

The plan body is left as written; it is the record of what was planned, and
editing it to match the result would erase the difference. Each phase gained an
`Outcome` section, and the "Context" section is flagged as the *before* state.

Verified per phase:

- **Phase 1 landed.** `scripts/go-coverage.sh`, `make coverage`, exclusions in
  one regex, E2E child-process coverage folded in via `GOCOVERDIR`. The floor
  went past its target: Makefile default **65**, and `e2e.yml` runs
  `make coverage GO_COVERAGE_MIN=68`, so **68 is the real gate**. No percentage
  is quoted in the docs — a hardcoded figure is wrong the moment a test lands.
- **Phase 2 landed.** Pins moved into `scripts/e2e-fixtures.env`, which is both
  the provisioning input and the CI cache key. The 45-second acceptance
  criterion is explicitly marked unverified — only CI timings can answer it.
- **Phase 3 landed.** Gateway and Chromium suites both exist as separate checks.
  Step 3 of the plan's gateway list (only *mutating* calls rejected) is
  superseded by ticket 01/09; the Outcome says the test asserts whatever that
  policy is and that this plan is not its specification, so the two tickets do
  not have to be merged in any particular order for the doc to be true.
- **Phase 4 landed.** `macos-15` pinned, nightly + dispatch + `v*` tags,
  `mlx-lm==0.31.3`, pinned model revision, non-empty completion asserted.
  Outstanding: the optional labeled-PR trigger was never implemented.
- **Phase 5 partially landed.** The numeric targets are met — total above the
  floor, and `core/control`, `core/runtime`, `core/profiles`,
  `core/modeldownload` all above 70%. But the areas it set out to fix are still
  the weakest (`adapters/tui`, `cmd`, `core/setup`, `platform/process`), and the
  "no 0%-covered function on a required process, auth, or cleanup path" clause
  has never been audited by anyone. Recorded as unverified rather than met.

The "CI and developer commands" block gained `make e2e-browser` and the CI order
was corrected to the four checks that actually exist, noting that coverage runs
*inside* the llama.cpp job because it must include the child processes' coverage.

### `docs/architecture-overview.md`

"Working on this repo" listed only `make test|lint|verify|generate|webui`. Added
`make coverage`, `make e2e`, `make e2e-browser`, `make e2e-live-mlx`, and a
pointer to the evidence-level definitions.

### PR #1 description

Rewritten: it described a bootstrap-only Phase 0+1 branch, while
`phase-01-bootstrap` is now the integration branch for the whole
release-readiness effort.

### Acceptance

- Every `make` command quoted in `docs/` is a real Makefile target, checked
  mechanically. The two remaining `make e2e-live` mentions are deliberate
  references to a *removed* target, both in past tense in the plan's history.
- `pnpm test:e2e` exists in `webui/package.json`.
- No doc claims hardware validation; the recorded-runs section is empty.
- `make test` and `make lint` green (docs-only change).

### For ticket 16

The release-receipt table shape is in `docs/hardware-validation.md` under
"Platform support versus released artifacts". Fill one row per published
artifact per release. Do not let a row inherit an evidence level from a
platform that was not itself proved.
