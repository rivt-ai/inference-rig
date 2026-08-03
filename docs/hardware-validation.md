# Evidence levels and hardware validation

What InferenceRig claims about a platform depends on what was actually run on
it. This document defines the four levels, records which platforms hold which
today, and keeps platform *support* separate from *released artifacts* — they
are different claims and drift apart.

Every command quoted here exists in the Makefile. Every claim below was checked
against the tree, not against previous prose.

## The four evidence levels

Each level is strictly stronger than the one above it. A lower level is never
evidence for a higher one — in particular, **contract tests are not hardware
proof**: they prove the code agrees with itself, not that an engine ran.

### 1. Contract verified

The backend passes the shared backend contract (`backends/backendtest`,
asserted by `backends/contract_test.go`) plus its own policy, renderer,
installer, catalog and lifecycle tests.

- Proves: the backend implements the registry contract and its rendering,
  install and catalog logic behaves.
- Does not prove: any engine binary exists, launches, or produces a token.
- Run with: `make test`.

### 2. Control-stack verified

The backend passes through the canonical profile store, control manager,
Unix-socket RPC service, generated client, runtime factory and artifact-plan
path in `test/control_integration_test.go`.

- Proves: the backend is reachable and drivable through the real control stack.
- Does not prove: the engine started. This layer uses an in-process runtime.
- Run with: `make test`.

### 3. CI-tested

A compiled InferenceRig starts a real engine binary on a CI runner, loads a
pinned real model through the control socket, and **generates at least one
token**. Fixtures are pinned by revision and SHA-256 in `scripts/e2e-fixtures.env`.

- Proves: the whole stack works on that runner's OS and architecture, against
  that engine version and that model.
- Does not prove: anything about a different accelerator, a larger model, or
  hardware nobody ran it on.
- Run with: `make e2e` (llama.cpp), `make e2e-browser` (the Chromium workflow
  over the same harness), `make e2e-live-mlx` (MLX).

A missing fixture **fails** these suites; it is never a skip
(`requireEnv` in `test/e2e/harness_test.go`). A skipped engine test and a
passing engine test are indistinguishable in a green check, which is the
ambiguity this layer exists to remove.

### 4. Hardware verified

A recorded run of the same suites on named physical hardware, with the result
written down under [Recorded hardware runs](#recorded-hardware-runs). A
hardware claim is valid only for the exact platform, accelerator, engine
version and model recorded. Nothing is hardware verified until it appears in
that section.

## Current evidence matrix

| Platform | Engine | Contract | Control stack | CI-tested | Hardware |
|---|---|---|---|---|---|
| linux/amd64 | llama.cpp | yes | yes | yes — every PR, `make e2e` + `make e2e-browser` on `ubuntu-latest` | not recorded |
| darwin/arm64 | MLX | yes | yes | yes — nightly, manual dispatch and `v*` tags, `make e2e-live-mlx` on `macos-15` | not recorded |
| linux/arm64 | either | yes | yes | no — nothing runs on arm64 Linux | not recorded |
| darwin/arm64 | llama.cpp | yes | yes | no — the macOS job runs MLX only | not recorded |
| linux/amd64 | MLX | yes | yes | no — MLX needs Apple Silicon | not recorded |

Contract and control-stack tests are portable Go: they pass wherever `make test`
runs and are **not** an engine-support claim for that platform. That is why they
read "yes" on rows whose CI column reads "no".

Windows is not a control-plane target: the canonical local transport is a Unix
socket.

## Platform support versus released artifacts

Two separate claims, tracked separately on purpose.

**Supported platforms** — what the control plane is intended to run on:
linux/amd64, linux/arm64, darwin/amd64, darwin/arm64.

**Released artifacts** — what a user can actually download today.
`scripts/package-release.sh` builds all four targets, each with a SHA-256
checksum, a CycloneDX SBOM and a GitHub build-provenance attestation attached
to the release; `.github/workflows/release.yml` gates publication on the MLX
job and a real E2E pass against the packaged linux/amd64 tarball.

| Target | Supported | Artifact published | Evidence level |
|---|---|---|---|
| linux/amd64 | yes | yes | CI-tested (llama.cpp) — the packaged tarball itself runs the E2E suite before every release publishes |
| linux/arm64 | yes | yes | control-stack verified — nothing runs the compiled binary on this architecture in CI |
| darwin/amd64 | yes | yes | control-stack verified — nothing runs the compiled binary on this architecture in CI |
| darwin/arm64 | yes | yes | CI-tested (MLX) — the macOS job is a required release gate, but runs on the source tree, not the packaged artifact |

There is no per-release machine-readable receipt: the workflow runs linked from
a release are the evidence trail. A row above with no CI-tested/hardware
evidence is exactly as strong as it reads — published does not mean proven on
that architecture.

## Running the suites

llama.cpp, provisioned automatically from the pinned manifest:

```sh
make e2e          # compiled processes, real llama.cpp, real GGUF, real tokens
make e2e-browser  # one Chromium workflow over the same harness
```

MLX on Apple Silicon, with a provisioned environment and a local model snapshot:

```sh
INFERENCERIG_LIVE_MLX_PYTHON=/path/to/venv/bin/python \
INFERENCERIG_LIVE_MLX_MODEL=/path/to/local/snapshot \
make e2e-live-mlx
```

`make e2e` and `make e2e-browser` need no variables: `scripts/provision-e2e-llamacpp.sh`
downloads and SHA-256 verifies the pinned llama.cpp build and GGUF into a local
cache. Provisioning failure is fatal by design.

## Recorded hardware runs

None yet.

Local Apple Silicon validation is deliberately reserved for the final release
check; `macos-15` CI carries MLX until then. To record a run, add a row with the
date, machine, OS version, accelerator, engine version, model revision and the
command that passed.
