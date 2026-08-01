# 09 — Enforce the gateway security policy

Type: task
Status: resolved
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

## Answer

Implemented ticket 01's policy as written. What later tickets need to know:

### Authentication

`mutatingProcedures` is gone. Every Connect procedure and `/mcp` require the
bearer token; `GET /health` and `GET /` (the app shell) stay open, as decided.

The guard moved from a Connect interceptor to an `http.Handler` wrapper
(`requireToken`) around the whole Connect handler. **This closed a real hole**:
`connect.UnaryInterceptorFunc` does not wrap streaming handlers, so `WatchEvents`,
`WatchLogs` and `WatchModelCatalog` would have stayed unauthenticated under a
unary-only guard — the machine's entire event and log stream. Anything adding a
guard to a Connect service in this repo should wrap the handler, not the unary
path. Connect clients map the plain 401 back to `CodeUnauthenticated`, so client
code is unaffected. The same gap existed in the e2e helper `bearer`, now a full
`connect.Interceptor`.

The descriptor-walking test is now `TestEveryProcedureRequiresAuth`: it walks
unary *and* streaming procedures and asserts 401 for each. There is no list left
to add an RPC to, so a new RPC cannot slip through unclassified.

### Token lifecycle

`ResolveAuthToken(configured, path)` resolves env → `run/gateway.token` (0600) →
generate-and-persist. `config.GatewayTokenPath()` is the path. The token now
survives restarts, which authenticated reads make mandatory.

`inferencerig web` prints `http://host:port/#token=…`; the app consumes the
fragment (`takeLaunchToken` in `webui/web/src/lib/session.ts`), stores it where
`api.ts` already looks, and strips it from the address bar. The paste field
remains as the manual fallback. Ticket 11's QA script can use the printed URL
as the login step.

### Insecure mode

`ValidateSecurity` is no longer warn-only: `disable_auth` with a non-loopback
`listen_addr` is a **load error** unless `security.allow_exposed_without_auth`
is also set. Two consequences for later tickets:

- The setup wizard now writes `allow_exposed_without_auth` when the operator
  confirms `remoteBindWarning`, or it would emit a config that refuses to load.
- Any doc, script or fixture that sets `disable_auth` on a non-loopback bind
  must set the second key too.

Posture is visible in four places: the startup message (now naming what is
exposed), `"auth":"required"|"disabled"` in the `/health` JSON, an `Auth` field
in the TUI gateway box, and a permanent web UI banner. The banner shows **only**
when the browser is on a non-loopback address, per ticket 01 — loopback insecure
mode must not nag. The UI learns the posture from the unauthenticated `/health`
route, because it must be readable before the UI holds a token.

`/health` carrying `auth` is the machine-readable hook for ticket 11's QA script
and for `doctor` when Milestone C graduates it.

### Origins

`AllowedOrigin string` → `security.allowed_origins []string`. A configured list
**replaces** the loopback default rather than extending it. `docs/reverse-proxy.md`
documents one nginx config (including `proxy_buffering off`, without which the
server streams stall), asserted by `TestGatewayBehindReverseProxy`. No
`X-Forwarded-*` trust. The `auth.go` origin-guard rationale comment is rewritten:
with header auth and no cookies the guard is no longer the primary defence, it
is what keeps *insecure mode* from being DNS-rebindable.

### Redaction

`runtime.RedactArgv` masks credential-shaped argv (`--api-key`, `*token*`,
`*secret*`, `*password*`, inline `=` and separate-value forms) in
`command.Display`, which is what reaches logs, events and API responses. It
matches shapes, not a per-engine flag list. Paths and the rest of the argv are
deliberately **not** redacted. `Argv` itself is untouched, so the engine still
launches with its real key. No second credential exists to separate: the control
socket is 0600 and presents none, so the gateway token remains the only
credential in the system.

### Testing note for later tickets

The control daemon owns `config.yaml` and rewrites it whole from its in-memory
copy (an autostart toggle is enough). A test that edits the config underneath a
running daemon will have its edit silently discarded — write the config before
`startControl`, as `TestGatewayInsecureMode` and `TestGatewayBehindReverseProxy`
do. This cost an hour; it is not obvious from the code.

### Verification

`make test`, `make lint`, `make webui` and the Playwright browser suite pass.
`make e2e`: `TestPublicGateway`, `TestGatewayInsecureMode` and
`TestGatewayBehindReverseProxy` pass (repeated runs); `TestCLIControlLifecycle`
could not run here because this sandbox blocks `huggingface.co`, so the GGUF
fixture cannot be downloaded — it fails at "model failed to load" with a
placeholder file, untouched by this change. CI is the gate of record for that
one.

### Breaking changes for the release notes

- Reads now require a token. Any script polling an RPC anonymously must send one.
- `security.allowed_origin` (string) is now `security.allowed_origins` (list).
- `disable_auth` on a non-loopback bind now fails to start without
  `allow_exposed_without_auth`.
