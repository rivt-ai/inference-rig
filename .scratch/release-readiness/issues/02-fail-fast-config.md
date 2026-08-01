# 02 — Fail fast on invalid configuration

Type: task
Status: resolved
Blocked by: none
Milestone: A
Roadmap: P0 #3

## Question

Make invalid configuration a startup error instead of a silent fallback to
defaults, and add `inferencerig config validate`.

`bootstrap/service.go:101` and `cmd/web.go:35` both do
`if loaded, err := config.Load(); err == nil { ... }` — so a syntax error, a
permission error or an unreadable file silently reverts security settings,
paths and listen addresses to defaults on a machine the operator believes is
configured.

Do:

- Use defaults **only** when the config file does not exist. Every other error
  (syntax, validation, permissions, I/O) fails startup.
- Error messages print the resolved config path and the exact failing field.
- Reject unknown keys (strict YAML decoding) — there are no users, so this
  costs nothing and catches typos that would otherwise read as defaults.
- Add `inferencerig config validate` to the existing `config` command group in
  `adapters/cli/commands.go`: exits non-zero with the same messages, prints the
  resolved path on success.
- Cover malformed YAML, unknown keys, an unreadable file, and an invalid
  security combination in tests.

Acceptance:

- A missing config file still starts with defaults.
- A malformed config file fails both `serve` and `web` with a message naming
  the path and the field.
- `inferencerig config validate` agrees with startup in every one of those
  cases.
- `make test` and `make lint` green.

Keep it lazy: this is an error-handling change plus one thin command, not a new
config package.

## Answer

Resolved 2026-07-31. `make test` and `make lint` green; `serve`, `web` and
`config validate` smoke-tested against a missing, a valid and a malformed file.

### The change is one function, not a package

`config.LoadOrDefault()` (`config/config.go`) is the only new surface: it calls
`Load()` and falls back to `Default()` on `fs.ErrNotExist` alone. Every other
failure is returned. All four callers that were swallowing errors now use it,
which is why the diff is small — the fix lives where the callers meet, not in
each caller:

- `bootstrap/service.go:100` and `cmd/web.go:34` — the two sites the ticket
  named, both `if loaded, err := config.Load(); err == nil`.
- `adapters/tui/services.go:263` (`autostartServices`) had the right intent but
  guarded with `os.IsNotExist(err)`, which does not unwrap. `LoadFile` wraps
  with `%w`, so that branch never fired and a machine with no config file
  failed autostart from the TUI. Fixed by the same call.
- `adapters/tui/services.go:307` (`listenAddress`) still falls back silently.
  Deliberate: it renders a URL in the TUI header, cannot fail startup, and has
  no error path to return one through. If ticket 04 or 10 gives the TUI a place
  to surface config errors, fold it in there.

### Already done, left alone

Strict decoding was **already in place** — `Parse` sets
`dec.KnownFields(true)`, and `TestParseRejectsUnknownField` covers it. The
ticket's "reject unknown keys" line was already satisfied; nothing to add. Same
for the error messages: `LoadFile` wraps with `parse config %q`, and the yaml
decoder names the line and field, so `parse config "…/config.yaml": yaml:
unmarshal errors: line 1: field listen_prot not found` needs no help.

### `inferencerig config validate`

Added to the existing `config` group (`adapters/cli/commands.go`). It calls the
same `LoadOrDefault`, so agreement with startup is structural rather than
tested into existence. It dials nothing — the test asserts that, because a
validate command that needs a running daemon is useless for the case it exists
to diagnose. `SilenceUsage: true` on that one command: the error is the output,
and a usage dump after it buries the field name.

Missing file prints `<path>: no config file; startup uses defaults` rather than
`ok`, so the verdict is not mistaken for "your file parsed".

### Not done: the invalid security combination

The ticket asks for a test covering one. There is none to cover —
`ValidateSecurity` is warn-only, and **ticket 01 §3 assigns the change that
makes it an error (plus `allow_exposed_without_auth`) to ticket 09**. Writing it
here would either duplicate that work or relitigate a settled decision. Ticket
09 owns it; the validation-fails-startup path is covered instead via
`log_archive_retention: -1h`, which exercises the same code path.

### Tests

- `config.TestLoadOrDefaultFallsBackOnlyWhenAbsent` — missing file yields
  defaults; malformed YAML, unknown key and invalid value each fail with the
  config path in the message. The unreadable case points the config env at a
  *directory* rather than chmod'ing a file to `0000`, because CI and the agent
  containers run as root, where a `0000` file is still readable and the test
  would silently stop testing anything.
- `cli.TestConfigValidateMatchesStartup` — missing, valid and broken file;
  asserts non-zero exit, the path in the message, and no dial.

### For ticket 09 and the future `doctor`

`LoadOrDefault` is the single entry point for "what config would startup use".
Anything that needs to report on configuration — `doctor` in Milestone C, the
posture fields in ticket 09 — should call it rather than `Load`, or the
missing-file case turns into a spurious error.
