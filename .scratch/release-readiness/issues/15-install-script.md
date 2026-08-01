# 15 — Install script with stable and dev channels

Type: task
Status: open
Blocked by: 14
Milestone: B
Roadmap: ODS #4

## Question

Give a new user one command that installs a verified InferenceRig.

Do — a single POSIX `sh` script, no dependencies beyond `curl`/`sha256sum`
equivalents:

- Detect OS and architecture; refuse clearly on anything outside the three
  supported targets.
- Resolve the channel (`stable` = latest release tag, `dev` per ticket 12) to
  an **immutable** tag before downloading; never install from a moving
  reference silently.
- Download the artifact and its checksum, verify before extracting, and verify
  the signature when the tooling is present (with a clear message when it is
  not).
- Install to a sensible user-writable prefix by default, honouring
  `INFERENCERIG_INSTALL_DIR`, and print the PATH line if the prefix is not on
  PATH.
- Handle the macOS quarantine behaviour ticket 13 identified.
- Support inspect-first usage: the README shows downloading and reading the
  script before running it, not only `curl | sh`.
- Be idempotent, and print what it is about to do before doing it.

Acceptance:

- Installing on a clean Linux container yields a working `inferencerig
  --version`.
- A tampered checksum aborts the install and leaves nothing behind.
- Re-running upgrades in place without duplicating anything.
- The script is exercised in CI against the published pre-release artifacts.

Keep it lazy: this is one script, not an installer framework. Uninstall is
`rm`; document it rather than building it.
