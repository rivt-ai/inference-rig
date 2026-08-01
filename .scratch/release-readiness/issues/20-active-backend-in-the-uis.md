# 20 — Render the active backend and replace in the TUI and web UI

Type: task
Status: claimed
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
