# 20 — Render the active backend and replace in the TUI and web UI

Type: task
Status: resolved
Blocked by: 04
Milestone: A
Roadmap: P2 #13

## Question

Ticket 03 §4 and §5 describe front-end behaviour that ticket 04 left undone,
because 04's own Do and Acceptance lists were entirely control-plane. Read
ticket 03's `## Answer` and ticket 04's `## Answer` first — the semantics are
settled, this is only rendering them.

Today both UIs call `StartRuntime` with no `replace` and render whatever comes
back, so a user who starts a second profile gets a raw conflict string and no
way forward. Everything needed is already on the wire: `GetInfo.active_backend`,
`StartRuntimeRequest.replace`, the `ResetRuntimes` RPC, and the transition
events on `WatchEvents`.

Do:

- Show the active backend. Profiles belonging to another backend stay listed but
  render dimmed and unstartable, with the reason ("MLX is active — reset to
  start llama.cpp profiles") and the reset action offered inline. Do not filter
  them out: a user who cannot find a profile they created will believe it was
  deleted.
- On a conflict that `replace` would resolve, confirm first and retry with
  `replace: true`, reusing each UI's existing destructive-action pattern.
- Surface the transitional states the slot now reports (`starting`,
  `activating`, `stopping`, `failed`, `orphaned`) rather than showing only
  running/stopped.

Acceptance:

- Starting a profile on a non-active backend offers reset inline in both UIs.
- Starting a second profile on an exclusive backend confirms, then replaces.
- `make test`, `make lint` and `pnpm run verify:web` green.

This must land before ticket 11 (Milestone A manual QA) can pass: the manual
script drives both UIs against a real llama.cpp profile.

## Answer

Resolved 2026-08-01. Both UIs now render `GetInfo.active_backend`, keep profiles
from other backends visible but dimmed and unstartable, explain the conflict,
and offer the existing confirmed-destructive flow for `ResetRuntimes` inline.

The TUI polls the all-profile `GetRuntimeStatus` form and renders each exact
slot state. Enter on an exclusive-backend replacement (or a router profile on
an incompatible listen address) confirms before sending `replace: true`; Enter
on a cross-backend profile confirms reset instead. The web UI keeps the same
state per profile, derives the aggregate transitional state from those rows,
and its existing exclusive-backend dialog now retries with `replace: true`.

Covered at the UI interaction seams and the typed Connect dispatch seam.
`make test`, `make lint`, and `pnpm run verify:web` pass.
