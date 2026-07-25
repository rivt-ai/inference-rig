# Agent Rules

InferenceRig is a neutral control plane merged from two upstream references
(llamarig, mlxrig). Read `docs/HANDOVER.md` first — it carries the full merge
spec, the locked decisions, and the current position in the stacked-PR build.

## Neutrality (non-negotiable)

- Shared packages must not import, name, or reference `llamarig`/`mlxrig`, or
  any llama.cpp/MLX-specific term. Engine specifics live only in
  `backends/llamacpp` and `backends/mlx`.
- Sources are read-only references. Never develop in them, merge their git
  history, or copy a whole repo and rename it.
- Port behavior into its intended final location and neutralize naming as you
  go. Port tests alongside behavior.

## Go Receiver Style

- For any Go type, use either pointer receivers or value receivers for all
  methods on that type. Never mix both on the same type.
- Prefer pointer receivers when methods mutate, the type holds synchronization
  primitives, or copying is undesirable. Value receivers only for small
  immutable value types.

## Quality Guard

- Run `make test` and `make lint` before committing.
- One exported `New*` constructor per concrete type; no `With` constructor
  variants (enforced by `constructor_guard_test.go`).
- Before adding logic, inspect existing packages and reuse the existing owner.
- A module-wide Go LOC budget (via a custom linter) is reinstated in a later
  phase; keep implementations lean in the meantime.

## Reuse-First Checklist

1. What existing package owns this behavior?
2. Which existing types/functions can be reused or extended?
3. Why is new logic necessary?
4. What can be removed or consolidated?
