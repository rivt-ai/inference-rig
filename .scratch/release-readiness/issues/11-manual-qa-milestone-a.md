# 11 — Milestone A gate: manual TUI and web UI QA

Type: task (HITL)
Status: open
Blocked by: 02, 04, 05, 06, 07, 08, 09, 10, 20
Milestone: A
Roadmap: quality gate

## Question

Prove Milestone A holds together for a human sitting in front of the TUI and
the web UI, against a real llama.cpp engine.

Two passes, in order.

**1. Agent dry-run (AFK).** Before handing anything to the human, drive
everything that can be driven:

- `make test`, `make lint`, `make coverage`, `make e2e`, `make e2e-browser` —
  all green, and record the numbers.
- Exercise the milestone's new behaviour through the compiled CLI against a
  real llama.cpp profile using the pinned fixtures
  (`scripts/provision-e2e-llamacpp.sh`): fail-fast config, state transitions,
  daemon-restart adoption, autostart, download resume, engine rollback, the
  gateway's secure and insecure postures.
- Capture web UI screenshots via the existing Playwright setup.
- Report what broke and fix it, or open a follow-up ticket, before step 2.

**2. Human script (HITL).** Write `docs/qa/milestone-a-manual.md`: a numbered
sequence the effort owner runs on Linux with a real llama.cpp profile, covering
what only a human can judge —

- TUI: first-run setup wizard, profile create/start/stop/restart, live status
  and event feed during a slow start, download progress and cancel,
  reconciliation display after killing and restarting the daemon, autostart
  state, insecure-mode warning visibility, error legibility.
- Web UI: token/auth flow from a cold browser, the same profile lifecycle,
  streaming logs, destructive-action confirmation, insecure-mode banner,
  behaviour when the daemon dies underneath the page.

Every step states the action and the expected observation. Keep it under ~30
steps — this gets run more than once.

Acceptance: the dry-run evidence is recorded in `## Answer`, and the effort
owner has run the script and signed off in the ticket. Do not resolve this
ticket on the agent pass alone. Milestone B does not start until this is
signed off.
