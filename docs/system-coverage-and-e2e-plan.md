# System Coverage and E2E Plan

Status: proposed for implementation after PR #1
Baseline: `6bad76a` (PR #1 plus the latest committed local fix), 2026-07-30

## Context

The current test suite is healthy at the package level, but it does not yet
provide a reliable system-level release signal.

Measured with all Go tests instrumenting all packages, then merging duplicate
coverage blocks and excluding generated RPC code, `backends/backendtest`, and
`webui`:

- Hand-written Go production coverage: **63.2%** (3,109 / 4,918 statements).
- Strong areas: profiles 82.0%, model downloads 82.5%, runtime 81.1%, model
  catalog 81.3%, config store 81.6%.
- Weak system-facing areas: RPC 52.6%, setup 45.2%, TUI 38.1%, process
  management 20.9%, commands 16.7%.
- The sole cross-package integration test exercises about 12% of the complete
  hand-written production statement set. It starts a real Unix-socket RPC
  server, but uses an in-process fake runtime and covers only a happy path.
- `make e2e-live` currently reports success while both tests skip on the
  GitHub Ubuntu runner. The workflow supplies no engine/model inputs; the MLX
  test also requires Apple Silicon. On draft PRs the whole job is skipped.
- The live tests stop after process readiness. They do not make an inference
  request.
- Web tests use Vitest/jsdom. Playwright is installed only for screenshot
  capture; there is no browser E2E.
- CI publishes no coverage artifact and enforces no coverage floor.

## Goals

1. Make every PR run through compiled InferenceRig processes, the control
   socket, real CLI commands, the public gateway, a pinned llama.cpp binary,
   and a tiny real GGUF model.
2. Cover one complete browser workflow against those real processes.
3. Turn hardware validation into an honest signal: a selected live job either
   provisions its prerequisites and runs an inference or fails.
4. Publish scoped Go coverage and prevent regression without rewarding tests
   for generated code.
5. Keep the suite deterministic, parallel-safe, and small enough for normal PR
   use.

## Non-goals

- Do not run large real models on every PR.
- Do not add an external coverage service.
- Do not chase 100% coverage or add tests for generated protobuf/Connect code.
- Do not duplicate backend correctness tests already owned by package tests.
- Do not make tests depend on a developer's installed engines, home directory,
  ports, or credentials.

## Test layers

| Layer | Runs | Purpose |
| --- | --- | --- |
| Package tests | Every PR | Exhaustive branches, parsers, state machines, and error mapping |
| Real llama.cpp process E2E | Every PR | Prove compiled binaries, a supported engine, a model, and user-facing transports work together |
| Browser E2E | Every PR | Prove one critical UI workflow against the real gateway |
| Apple Silicon MLX validation | Scheduled, manual, and release | Prove the MLX backend can load a real model and infer |

## Phase 1: honest Go coverage

### Implementation

Add `scripts/go-coverage.sh` and a `make coverage` target.

The script must:

1. Run the complete Go suite with atomic cross-package instrumentation.
2. Merge identical coverage blocks emitted by different test binaries, treating
   a block as covered when any test binary executes it.
3. Exclude:
   - `core/rpc/gen/**`
   - `backends/backendtest/**`
   - `webui/**`
4. Print the total and per-package percentages.
5. Write a merged profile and HTML report under `artifacts/coverage/`.
6. Fail below `GO_COVERAGE_MIN`, initially **60%**.

Use only the Go toolchain plus POSIX shell/awk. Do not add a coverage
dependency. Keep the threshold in one Makefile variable so it can be ratcheted
without editing the script.

Add a CI step that runs `make coverage` and uploads the text profile and HTML
report even when the threshold fails.

### Acceptance

- Repeated local runs report the same statement totals.
- Generated code cannot raise or lower the result.
- A deliberately uncovered production branch lowers the result.
- The initial gate passes at the measured baseline and fails below 60%.
- Raise the floor to **65%** after Phases 2 and 3 land.

## Phase 2: real-engine compiled-process E2E

### Pinned llama.cpp and model

Add `scripts/provision-e2e-llamacpp.sh`. It downloads and verifies:

- official llama.cpp CPU build `b9637`,
  `llama-b9637-bin-ubuntu-x64.tar.gz`, SHA-256
  `a50ee14f021a9d8e92e30f622f7e3be1318ee1125bb9a9ba8d2025388df48743`;
- `mradermacher/SmolLM2-135M-Instruct-GGUF` at revision
  `34d6b157ff9fb55285116fa8524deb0d90a4982e`, file
  `SmolLM2-135M-Instruct.Q4_K_M.gguf` (about 101 MB), SHA-256
  `ee1bbe2dd452b84feaf94a02150c04f10ba13f6840c389bab0f70f99c21ed02a`.

Keep these pins in one versioned manifest consumed by the script and CI cache
key. Download to a user or CI cache, verify before extraction/use, and expose
the resolved `llama-server` and model paths to the test. Never fall back to
`latest`, and never silently skip when provisioning fails.

This is deliberately a real supported engine and model. Do not add an
InferenceRig-specific fake server.

### Harness

Add `test/e2e/harness_test.go` to:

- build coverage-instrumented InferenceRig binaries;
- require the provisioned llama.cpp executable and GGUF paths;
- allocate ports by binding listeners rather than guessing;
- give every test its own `INFERENCERIG_HOME`, config, socket, PATH, logs, and
  model file;
- start child processes with `GOCOVERDIR` so their execution contributes to the
  system coverage artifact;
- wait on observable readiness with bounded polling;
- capture stdout/stderr on failure;
- stop every process and assert PID/socket cleanup.

Prefer foreground child processes owned by the harness. Do not use sleeps as
readiness checks.

### One canonical CLI/runtime flow

Add `TestCLIControlLifecycle`:

1. Start the compiled control daemon.
2. Verify `health`, `backend list`, and `info` through the compiled CLI.
3. Create a llama.cpp profile from a YAML file through `profile create`.
4. Resolve the model and verify a single-file plan.
5. Start the profile through `runtime start`; the real `llama-server` process
   must load the GGUF model and become ready.
6. Verify status and generated `models.ini`.
7. Send a deterministic, short inference request and assert at least one
   generated token.
8. Restart, stop, and verify stopped status.
9. Verify the audit event sequence and clean shutdown.

Use the existing unit/integration suite for malformed profiles and backend
matrix coverage. The process E2E needs one representative backend path.

### Acceptance

- Runs on a stock GitHub Ubuntu runner. After a warm cache, no external
  downloads are required.
- Exercises the compiled root command, CLI adapter, control socket, RPC service,
  profile store, materialization, supervisor, PID handling, real llama.cpp
  process, model loading, and inference.
- Finishes in under 45 seconds on a warm CI runner.
- A broken command registration, socket path, argument render, readiness probe,
  inference request, or shutdown path makes the test fail.

## Phase 3: public gateway and browser

### Gateway flow

Add `TestPublicGateway` to the same process harness:

1. Start control and `inferencerig web` with a fixed test token.
2. Assert `/health` succeeds.
3. Assert an unauthenticated mutating Connect call and MCP call are rejected.
4. Assert the authenticated calls succeed.
5. Assert an unapproved browser origin is rejected.
6. Call `tools/list` and one read-only MCP tool.
7. Open `/` and assert the built application shell is served.
8. Stop the gateway and assert PID cleanup.

This test should cover the currently unexecuted public stream bridge with one
short watch request that is cancelled cleanly.

### Browser flow

Add a Playwright test under `webui/e2e` and a `pnpm test:e2e` command. Reuse the
Go process harness through environment-provided URLs/token; do not mock the
Connect client.

Keep one browser scenario:

1. Load the application and authenticate.
2. Create a profile for the provisioned llama.cpp engine and GGUF model.
3. Start it and observe running status.
4. Stop it and observe stopped status.
5. Confirm a destructive profile action before deletion.

Run Chromium only on PRs. Cross-browser coverage is not justified until a
browser-specific defect appears.

### Acceptance

- The browser test uses the real built Svelte app, public HTTP server, Connect
  transport, control daemon, llama.cpp process, and model.
- No network request escapes localhost.
- Failure includes Playwright trace and screenshot artifacts.
- `verify:web` remains the fast component/unit gate; browser E2E is a separate
  target.

## Phase 4: Apple Silicon backend validation

Phase 2 already makes real llama.cpp CPU inference a required PR check. Replace
the remaining misleading live job with an explicit MLX job.

### llama.cpp

- `make e2e` provisions the pinned binary/model and runs real inference on
  every PR.
- Reuse that same test on nightly and release workflows; do not create a second
  direct-backend test that bypasses control/RPC.

### MLX

- Use GitHub's standard `macos-15` runner. For a public repository this is an
  Apple Silicon M1 VM (3 CPU, 7 GB RAM) and does not require a paid larger
  runner. Pin the OS label rather than `macos-latest`.
- Create a job-local virtual environment and install the backend's managed
  version, currently `mlx-lm==0.31.3`.
- Download
  `mlx-community/Qwen2.5-0.5B-Instruct-4bit` (about 278 MB) at revision
  `a5339a4131f135d0fdc6a5c8b5bbed2753bbe0f3` into the runner's temporary
  directory. Pass the local snapshot path to the test so a later model update
  cannot change the run.
- Cache the Hugging Face download by repository and revision, but never execute
  a cached Python environment.
- Run only the MLX live test.
- Start MLX through the compiled control/RPC path, POST a short request to
  `/v1/chat/completions`, and assert that the response contains at least one
  generated token.

The workflow shape should be:

```yaml
mlx:
  runs-on: macos-15
  timeout-minutes: 15
  steps:
    - uses: actions/checkout@v7
    - uses: actions/setup-go@v7
      with:
        go-version-file: go.mod
    - name: Provision pinned MLX environment and model
      shell: bash
      run: |
        python3 -m venv "$RUNNER_TEMP/mlx-venv"
        "$RUNNER_TEMP/mlx-venv/bin/python" -m pip install "mlx-lm==0.31.3"
        "$RUNNER_TEMP/mlx-venv/bin/python" -c \
          'from huggingface_hub import snapshot_download; snapshot_download(repo_id="mlx-community/Qwen2.5-0.5B-Instruct-4bit", revision="a5339a4131f135d0fdc6a5c8b5bbed2753bbe0f3", local_dir="'"$RUNNER_TEMP"'/mlx-model")'
    - name: Run MLX inference E2E
      env:
        INFERENCERIG_LIVE_MLX_PYTHON: ${{ runner.temp }}/mlx-venv/bin/python
        INFERENCERIG_LIVE_MLX_MODEL: ${{ runner.temp }}/mlx-model
      run: make e2e-live-mlx
```

If GitHub-hosted Apple Silicon becomes unavailable, use a dedicated M-series
Mac runner with
`runs-on: [self-hosted, macOS, ARM64, inferencerig-mlx]`. Restrict its runner
group to this repository, run it as an unprivileged account, and do not expose
it to code from fork pull requests. A paid `macos-15-xlarge` runner is not
needed for the selected 278 MB model.

Missing prerequisites must be a workflow setup failure, not `t.Skip`. Keep MLX
in its own `e2e-live-mlx` target so selecting it cannot create a skip for
llama.cpp or vice versa.

Run the MLX job on:

- nightly schedule;
- manual dispatch;
- release tags.

It may also run on labeled PRs when hardware capacity is available, but should
not block ordinary PRs.

### Acceptance

- A green job proves at least one generated token, not only readiness.
- Job output records engine, model, OS, architecture, and accelerator versions.
- Artifacts/checksums are pinned; an upstream `latest` change cannot silently
  alter the result.
- No live workflow can pass with zero executed tests.

## Phase 5: close targeted gaps

After process E2E coverage is merged, use the report to add only high-value
tests for branches that cannot be reached economically through E2E:

- RPC validation/error-code mapping and stream cancellation.
- Setup confirmation and remote-bind warnings.
- Process start/stop escalation and failure cleanup.
- Public HTTP stream forwarding.
- Download cancellation and partial-file cleanup.
- TUI action dispatch for start/stop/download/cancel.

Targets after this pass:

- total hand-written Go production coverage: **at least 65%**;
- `core/control`, `core/runtime`, `core/profiles`, and
  `core/modeldownload`: **at least 70% each**;
- no 0%-covered function on a required process, auth, or cleanup path.

Do not create package floors for presentation-only formatting code.

## CI and developer commands

Add these stable entry points:

```text
make test                 # existing fast Go suite
make coverage             # scoped report and threshold
make e2e                  # provision and test real llama.cpp inference
pnpm test:e2e             # browser test against a supplied/running harness
make e2e-live-mlx         # provisioned real engine
```

PR CI order:

1. existing Go and web verification;
2. scoped coverage;
3. real llama.cpp process E2E;
4. Chromium E2E.

Keep each as a separate check so failures identify the broken layer.

## Delivery sequence

1. Coverage script, report artifact, and 60% non-regression floor.
2. Pinned llama.cpp/model provisioning, process harness, real inference, and
   CLI/runtime lifecycle.
3. Gateway/auth/MCP test and 65% coverage ratchet.
4. One Chromium workflow.
5. Provisioned MLX workflow with an inference assertion.
6. Targeted gap tests only where the resulting report still shows critical
   uncovered behavior.

Each step should be independently reviewable and leave `make test` and
`make lint` green.

## Definition of done

Coverage is considered good enough for this phase when:

- every PR proves one real compiled application lifecycle and one real browser
  lifecycle using pinned llama.cpp and a pinned GGUF model;
- generated code is excluded from the published 65%+ Go result;
- critical control/runtime/storage packages stay above 70%;
- scheduled/release live jobs cannot pass by skipping;
- each supported engine completes a real inference in its matching hardware
  job;
- failures preserve enough logs, coverage, and browser artifacts to diagnose
  without rerunning interactively.
