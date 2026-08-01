# 06 — Make profile autostart operational

Type: task
Status: resolved
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

## Answer

Resolved 2026-08-01.

Bootstrap validates the complete configured set, reconciles surviving
processes, and only then starts autostart profiles in lexical order. Validation
rejects mixed backends, more than one profile on an exclusive backend, and
router profiles with different listen addresses. Enabling autostart through the
control API applies the same validation before rewriting config.

Each profile gets three attempts with 250ms then 500ms backoff. An adopted
profile is recognized through `RuntimeStatus` and is not restarted. Exhausting
the budget leaves the daemon healthy and preserves ticket 04's failed-start
policy: each attempt emits its `failed` transition and releases the empty slot.
A final `runtime.autostart` event names the profile, attempt count and error;
CLI event output receives it through the existing wire contract, while TUI and
web event views render the profile and detail. A later profile still starts when
an earlier one fails, so partial startup remains usable and visible.

`inferencerig service generate systemd|launchd`, `service install`, and
`service uninstall` use embedded native templates and the standard per-user
locations. Install/reinstall/uninstall are idempotent around absent or already
loaded registrations. The systemd unit uses bounded restart limiting; the
LaunchAgent uses `RunAtLoad` without `KeepAlive`, avoiding an unbounded crash
loop. Final validation passed `systemd-analyze verify`, XML parsing, the web
build and 117 web tests, `make test`, and `make lint` at 12,264 production Go
lines under the original nearest 50-line ceiling of 12,300. Review hardening
raised the integrated total to 12,307 and the ceiling to its nearest 50, 12,350.
macOS execution remains for
the planned final hardware validation.
