# InferenceRig

A neutral local control plane for language-model inference servers, with
pluggable backends (llama.cpp and Apple MLX). Define one canonical YAML profile
format, then start, stop, switch, and monitor any backend from a shared control
daemon, CLI, TUI, web GUI, or MCP client.

> **Status:** implementation stack complete and under review. InferenceRig was
> assembled in a fresh repository from two upstream reference implementations
> ([llamarig](https://github.com/antonikliment/llamarig),
> [mlxrig](https://github.com/antonikliment/mlxrig)) as a neutral core with
> engine-specific behavior isolated behind backend interfaces. See
> `docs/HANDOVER.md` for the stacked build and
> `docs/hardware-validation.md` for verified support levels and live checks.
