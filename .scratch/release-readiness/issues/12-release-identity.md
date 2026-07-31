# 12 — Decide release identity, channels and signing

Type: grilling
Status: open
Blocked by: none
Milestone: B
Roadmap: P0 #4, ODS #4

## Question

Settle the questions the release workflow (ticket 14) cannot answer for itself,
because they are the owner's call, not an engineering trade-off:

1. **Version scheme** — is the first release `v0.1.0`, `v1.0.0`, or a
   pre-release like `v1.0.0-rc.1`? What does the version imply about stability
   promises, given there are no users yet?
2. **Repository visibility** — is the GitHub repo public at release time?
   (Determines whether the free `macos-15` runner is available for the MLX gate
   and whether provenance attestation is publicly verifiable.)
3. **Signing identity** — keyless signing via GitHub OIDC, or a maintained key?
   Who is the identity, and how does a user verify it?
4. **Channels** — what distinguishes `stable` from `dev` for the install script
   (latest tag versus latest main build?), and does `dev` publish artifacts at
   all?
5. **What blocks a release** — which of the gates (unit, lint, coverage floor,
   llama.cpp E2E, browser E2E, MLX job, manual QA sign-off) are hard blockers
   versus advisory, and does a release claiming MLX support require a hardware
   run?
6. **Licence obligations** — the roadmap notes ODS is Apache-2.0 and this repo
   MIT. Confirm nothing Apache-licensed was copied without its notices, or
   record what carries which notice.

Use `/grilling`. This is a short session — the answer is a list of settled
facts ticket 14 reads.
