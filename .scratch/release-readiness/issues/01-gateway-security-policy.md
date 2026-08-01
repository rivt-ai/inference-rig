# 01 — Decide the gateway security model

Type: grilling
Status: resolved
Blocked by: none
Milestone: A
Roadmap: P0 #5

## Question

What is the gateway's authentication and exposure policy, precisely enough that
ticket 09 can implement it without further judgement calls?

Today (`adapters/public_http/auth.go:25`) only mutating procedures require the
bearer token. Every read is anonymous, which exposes profiles, installed
models, runtime state, telemetry, logs and audit records to anything that can
reach the port. The comment says this "matches how the gateway behaved before"
— that is the thing to settle, not preserve.

Decide and record:

1. Authenticate **all** control RPCs by default, or introduce explicit
   `read`/`manage` scopes? (There are no users, so the cheapest correct default
   wins — do not build scopes to avoid a breaking change.)
2. If reads become authenticated, how does the web UI get a token before it can
   render? (Options to weigh: token in the URL fragment on launch, a
   `inferencerig web --open` flow that writes a one-time cookie, a login form.)
3. Is anonymous read opt-in via config, and what is the flag called?
   **Constraint from the effort owner: an insecure mode must remain
   possible — the secure posture is the default, not the only option.** Decide
   what turns it on (config key and/or flag) and how loudly it warns: at
   startup, in `health`, in the TUI header, as a persistent banner in the web
   UI. Warnings must be impossible to miss and must name what is exposed.
4. Do we refuse to start on a non-loopback bind without auth? What is the
   escape hatch for a deliberate reverse-proxy deployment, and what does the
   trusted-origin config look like?
5. Are control credentials separated from inference credentials (the engine's
   own API key), and where does each live?
6. What must be redacted from logs, events and API responses — tokens, secrets,
   absolute paths, engine argv?

Use `/grilling` and `/domain-modeling`. Read `adapters/public_http/auth.go`,
`server.go`, and `config/config.go` before asking anything the code already
answers.

The answer is a written policy in the `## Answer` section, plus an ADR under
`docs/adr/` if the decision deserves one. No implementation in this ticket.

## Answer

Resolved 2026-07-31 with the effort owner. This is the spec for ticket 09 —
implement it, do not re-litigate it.

### Facts the code already settled (verified, not assumed)

- The control socket is `0600` inside a `0700` directory
  (`core/rpc/listener.go:30-44`), so the CLI and TUI are protected by
  filesystem permissions and never present a credential. **The gateway is the
  only network-reachable surface, and the gateway token is the only credential
  in the system.** Roadmap P0 #5's "separate control credentials from inference
  credentials" is therefore mostly moot — record it as such rather than
  building a second credential.
- Insecure mode already exists: `config.Security.DisableAuth`,
  the startup warning at `cmd/web.go:44`, and `config.WarnIfExposed`
  (`config/config.go:161`).
- The web UI already sends `Authorization: Bearer` from a persisted session
  token (`webui/web/src/lib/api.ts:28`) and already has the paste field
  (`webui/web/src/components/shell/AppShell.svelte:152`). No login UI needs
  inventing.

### 1. Authenticate everything

Delete `mutatingProcedures` (`adapters/public_http/auth.go:25`) and the
descriptor-walking test in `server_test.go` that exists only to police it.
Every Connect RPC and the `/mcp` endpoint require the bearer token.

Two deliberate exceptions, because "everything" cannot be literal:

- `GET /health` stays unauthenticated. Container healthchecks, load balancers
  and shell scripts cannot hold a token, and the response carries only `ok`,
  the service name, and the new auth-posture field.
- `GET /` (the static app shell) stays unauthenticated. It has to serve the UI
  that supplies the token. It contains no user data.

Rejected: read/manage scopes. Nobody asked for them, and they preserve the
per-RPC classification problem that the deletion removes.

### 2. Token lifecycle — no login system

Rejected: password login with session cookies. For a loopback single-user tool
a human-chosen password is weaker than `crypto/rand`, and it would add a login
form, session store, hashing, a set-password path and CSRF reasoning for a UX
gain that a launch URL delivers for ~15 lines.

Resolution order for the gateway token:

1. `INFERENCERIG_CONTROL_TOKEN` (via `cfg.Security.AuthTokenEnv`);
2. `${INFERENCERIG_HOME}/run/gateway.token`, mode `0600`;
3. generate with `crypto/rand` and write that file.

The token now **survives restarts** — required, because with reads
authenticated a per-run token means a blank dashboard after every restart.

`inferencerig web` prints a clickable `http://127.0.0.1:<port>/#token=…`. The
app reads the fragment, stores it where `api.ts` already looks, and strips the
fragment from the address bar. A fragment is never transmitted to the server,
so it cannot appear in access logs — a query parameter would. The paste field
stays as the manual fallback.

### 3. Insecure mode

Stays available; the secure posture is the default, not the only option.

- `disable_auth` + loopback bind: warn and serve. Unchanged.
- `disable_auth` + non-loopback bind: **startup error** naming both keys,
  unless `security.allow_exposed_without_auth: true` is also set.

`ValidateSecurity` (`config/config.go:156`) stops being warn-only and returns a
real error for that combination; the `// ponytail: warn-only by request`
comment there is now superseded and should be replaced. Two deliberate keys
means nobody reaches an exposed unauthenticated gateway by pasting a config
snippet.

### 4. Posture visibility

The posture must be discoverable by someone who did not watch the terminal at
startup (e.g. a systemd-managed daemon):

- startup message — keep as-is;
- `"auth":"disabled"` field in the `/health` JSON (`server.go:106`), so
  scripts, monitoring and the ticket 11 QA script can assert it;
- a badge in the TUI header while the gateway is unauthenticated;
- a persistent web UI banner naming what is exposed — **shown only when the
  browser is on a non-loopback address**. Loopback insecure mode is the normal
  single-user case and must not nag.

No audit event (considered, dropped as redundant).

### 5. Redaction — credentials only

Mask the gateway token, Hugging Face token, engine API keys, and
credential-shaped argv flags (`--api-key`, `*_token`) wherever they appear:
logs, events, API responses, future diagnostic bundles.

**Do not** redact paths or the rest of engine argv, despite the roadmap's
wording. They are the primary signal when a launch fails, the log reader owns
the machine, and redacting them degrades the manual QA scripts and the future
`doctor` for no gain against an attacker who already has log access.

### 6. Origins and reverse proxies

`AllowedOrigin` becomes a **list** (`allowed_origins`); one line more than a
string, and it removes the arbitrary single-origin limit that bites anyone
serving on both a hostname and an IP. `disable_origin_check` stays for proxies
that terminate origin themselves.

No `X-Forwarded-*` trust: nothing in the codebase consumes client IP, so a
trusted-proxy chain would be unused machinery.

Document one nginx or Caddy config and assert it in
`test/e2e/gateway_test.go`.

**Rewrite the rationale comment at `auth.go:100`.** With header-based auth and
no cookies, a malicious page has no ambient credential to spend, so the origin
guard is no longer the primary defence. It stays because it is what keeps
*insecure mode* from being DNS-rebindable.

### Consequences

- Ticket 09 grows slightly: token file, launch-URL fragment handling, three
  posture surfaces, origin list. Still one session.
- No new tickets.
- Breaking change for the release notes: reads now require a token by default.
