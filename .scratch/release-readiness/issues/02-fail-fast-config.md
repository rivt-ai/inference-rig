# 02 — Fail fast on invalid configuration

Type: task
Status: claimed
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
