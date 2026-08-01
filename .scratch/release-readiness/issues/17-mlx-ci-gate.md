# 17 — Make the MLX CI job a real release gate

Type: task
Status: open
Blocked by: 16
Milestone: B
Roadmap: P0 #4, phase 4 of the E2E plan

## Question

Make `.github/workflows/mlx.yml` prove MLX inference, and make it gate any
release claiming MLX support.

`make e2e-live-mlx` and the workflow exist — start by reading them and
`test/e2e/mlx_live_test.go` and reporting what they currently do. Do not assume
the plan in `docs/system-coverage-and-e2e-plan.md` §4 was implemented as
written.

Bring it to:

- `runs-on: macos-15` pinned (not `macos-latest`), job-local venv, the pinned
  `mlx-lm` version and the pinned model repo **and revision** downloaded to the
  runner temp — never a moving reference.
- Started through the compiled control/RPC path, not by calling the backend
  directly.
- Asserts at least one **generated token** from `/v1/chat/completions`, not
  just readiness.
- A missing prerequisite is a workflow failure, never `t.Skip`. No live job may
  pass with zero executed tests.
- Job output records engine, model, OS, architecture and accelerator versions,
  and feeds ticket 16's release receipt.
- Runs on nightly schedule, manual dispatch and release tags; it does not block
  ordinary PRs.

Note: pre-final validation is CI-only by the owner's decision. The local Apple
Silicon run happens once, in ticket 18.

Acceptance:

- A green run demonstrably generated a token (assert on the response body).
- Deliberately breaking the model path fails the job rather than skipping it.
- A release claiming MLX support cannot publish without this job green.
