# 10 — Correct documentation and evidence levels

Type: task
Status: claimed
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
