# 18 — Milestone B gate: installed-artifact manual QA

Type: task (HITL)
Status: open
Blocked by: 15, 16, 17
Milestone: B
Roadmap: quality gate

## Question

Prove the release a stranger downloads is the one we tested.

Same two-pass shape as ticket 11, but starting from the install script rather
than the repo.

**1. Agent dry-run (AFK).** On a clean Linux environment: install via the
script, verify the checksum and signature by the documented commands, run the
setup wizard, create and start a real llama.cpp profile, infer, restart the
daemon and confirm adoption, stop, uninstall. Record every command and its
output. Capture web UI screenshots from the installed build (not a dev server).

**2. Human script (HITL).** Write `docs/qa/milestone-b-manual.md` — the
first-run experience judged by a human:

- The install command as a new user runs it, including reading the script
  first.
- First launch with no config at all: does the TUI explain what to do next?
- The web UI from a cold browser on the installed build: auth flow, first
  profile, first inference, first log stream.
- Deliberate mistakes: wrong token, bad model path, port already taken,
  insecure mode. Is each message actionable?
- The release page itself: are the artifacts, checksums, receipt and
  verification instructions legible?

Acceptance: dry-run evidence in `## Answer`, and the effort owner has run the
script and signed off. Milestone C work does not start until this is signed
off.

When this resolves, graduate the Milestone C items from the map's **Not yet
specified** section into tickets — by then the state machine, reconciliation
vocabulary, config schema and release channels they depend on all exist.
