# Hardware validation

InferenceRig reports support at three evidence levels:

1. **Contract verified** — the real backend passes the shared backend contract
   plus its focused policy, renderer, installer, catalog, and lifecycle tests.
2. **Control-stack verified** — both real backend implementations pass through
   the same canonical profile store, control manager, Unix-socket RPC service,
   generated client, runtime factory, and artifact-plan path in
   `test/control_integration_test.go`.
3. **Hardware verified** — `make e2e-live` starts a real engine, waits for its
   HTTP readiness endpoint through the shared supervisor, checks running state,
   and stops it cleanly. A hardware claim is valid only for the exact platform,
   accelerator, engine version, and model recorded after this command passes.

## Current support matrix

| Backend form | Supported host | Contract | Control stack | Hardware |
|---|---|---:|---:|---|
| Single-file artifacts | Linux and macOS on the accelerators selected by the managed installer | verified | verified on Linux amd64 | validation harness available; no hardware result recorded in this workspace |
| Directory snapshots | Apple Silicon (`darwin/arm64`) with a compatible managed Python environment | verified | verified on Linux amd64 without starting the engine | validation harness available; requires Apple Silicon, no hardware result recorded in this workspace |

Linux amd64 is the current CI host. The directory-snapshot control-stack test is
portable policy validation; it is not an engine-support claim for Linux.
Windows is not currently a control-plane target because the canonical local
transport uses Unix sockets.

## Run live validation

Single-file backend:

```sh
INFERENCERIG_LIVE_LLAMACPP_BIN=/path/to/llama-server \
INFERENCERIG_LIVE_LLAMACPP_MODEL=/path/to/model.gguf \
make e2e-live
```

Apple Silicon directory backend:

```sh
INFERENCERIG_LIVE_MLX_PYTHON=/path/to/venv/bin/python \
INFERENCERIG_LIVE_MLX_MODEL=owner/model \
make e2e-live
```

When the variables are absent, the corresponding live test is explicitly
skipped. Record a passing run below before describing a platform as hardware
verified.

## Recorded hardware runs

None yet. The current build environment is Linux amd64 and has no real engine
binary or model installed.
