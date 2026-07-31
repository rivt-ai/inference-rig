# 06 — Make profile autostart operational

Type: task
Status: open
Blocked by: 05
Milestone: A
Roadmap: P0 #2

## Question

Actually start autostart profiles at daemon bootstrap, and integrate with the
OS service managers.

Autostart names are persisted and displayed (`core/control/operations.go:148`,
`AutostartProfiles`) but `bootstrap` never starts them.

Do:

- Reconcile existing processes (ticket 05) **before** starting configured
  profiles.
- Start autostart profiles in deterministic order.
- Apply ticket 03's concurrency policy: define what happens when several
  autostart profiles target a single-active backend, and reject or explain the
  impossible combination during validation rather than at boot.
- Bounded retries with backoff — no infinite crash loop.
- Report partial startup clearly: which started, which failed, why, through
  events and the front ends.
- Add optional systemd user-unit and macOS LaunchAgent integration (generate
  and install the unit; do not write a service manager). Keep it to a template
  plus an `install`/`uninstall` command path — the laziest thing that gives the
  user a daemon at login.

Acceptance:

- A configured autostart profile is running after daemon start, with no manual
  step.
- A profile that cannot start leaves the daemon healthy and the failure
  visible.
- Retry stops after the bound; the state machine shows `failed`.
- The generated systemd unit starts the daemon on a Linux host; the LaunchAgent
  plist is generated and validated (macOS execution can wait for the final
  hardware validation).
- `make test` and `make lint` green.
