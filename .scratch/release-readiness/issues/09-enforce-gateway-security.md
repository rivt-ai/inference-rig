# 09 — Enforce the gateway security policy

Type: task
Status: open
Blocked by: 01
Milestone: A
Roadmap: P0 #5

## Question

Implement whatever ticket 01 decided. Read its `## Answer` first — that is the
spec; do not re-litigate it here.

Expected shape (confirm against ticket 01 before starting):

- Secure by default: control RPCs authenticate, anonymous read is opt-in.
- **Insecure mode stays available** and is loud about it — a startup warning
  naming exactly what is exposed, plus a visible indicator in `health`, the TUI
  and the web UI. Someone running insecure must never be able to believe they
  are protected.
- Non-loopback bind without auth is refused unless explicitly opted in.
- Control credentials separate from inference credentials.
- Redaction of tokens, secrets, sensitive paths and engine argv from logs,
  events and API responses.
- Reverse-proxy and trusted-origin configuration, with tests.

Touch points: `adapters/public_http/auth.go` (`mutatingProcedures`,
`requireToken`, `originGuard`), `server.go`, `config/config.go`, the web UI
token flow in `webui/`, and `test/e2e/gateway_test.go`.

Acceptance:

- The existing descriptor-walking test in `adapters/public_http/server_test.go`
  is updated so a new RPC still cannot slip through unclassified.
- `test/e2e/gateway_test.go` asserts unauthenticated **reads** are rejected in
  the default posture and accepted in insecure mode, with the warning present.
- The web UI can still reach a usable state — a user who opens the browser gets
  a working token path, not a blank screen.
- `make test`, `make lint`, `make e2e` and `make e2e-browser` green.
