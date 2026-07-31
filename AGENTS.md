# Agent Rules

InferenceRig is a neutral control plane merged from two upstream references
(llamarig, mlxrig).

## Start here (required)

Read `docs/architecture-overview.md` before touching code. It maps the layers,
the import direction, and the entry-point file for each area, so you land in the
package that already owns the behavior instead of adding a parallel one — which
is what the Reuse-First Checklist below asks of you.

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

- Commit early and often — one small, self-contained commit per coherent
  change, not one commit at the end of a task. Small commits stay reviewable
  and keep a broken step from burying the working ones.
- Run `make test` and `make lint` before committing.
- `make test` and `make lint` must **pass** before pushing. A local commit may
  be a work-in-progress; a pushed branch may not. Never push a red branch, and
  never skip, disable, or weaken a test to make the gate go green.
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

## Agent skills

### Issue tracker

Issues and specs live as markdown files under `.scratch/<feature>/`. See `docs/agents/issue-tracker.md`.

### Domain docs

Single-context: `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.
