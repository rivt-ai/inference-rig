# 12 — Decide release identity, channels and signing

Type: grilling
Status: resolved
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

## Answer

Resolved 2026-07-31 with the effort owner. Settled facts for ticket 14.

### What `release.yml` already does (verified, not assumed)

More than the ticket assumed. It already enforces release-from-`main`, requires
a prerelease SemVer tag by regex, blocks until the `Test` and `Lint` check-runs
pass on the release SHA, builds with `CGO_ENABLED=0`, stamps version/commit/
commit-time through `internal/buildinfo`, tars with LICENSE and README,
generates `SHA256SUMS`, smoke-tests the extracted binary, and publishes with
`--prerelease --generate-notes`.

Missing: linux/arm64 and darwin/arm64 (amd64 only), SBOM, provenance, and any
gate beyond Test and Lint.

`CGO_ENABLED=0` already working is a useful signal for research ticket 13 — a
plain `GOOS`/`GOARCH` matrix is likely all the cross-compilation this needs.

### 1. Public at first release

The repo is currently **private** (`gh repo view` → `PRIVATE`), MIT, with no
tags. Flip it public before tagging `v0.1.0`.

This is load-bearing for two other tickets: a private repo's release assets need
an authenticated download, so ticket 15's `curl … | sh` one-liner cannot work as
written; and Actions minutes are billed, with macOS carrying the highest
multiplier, which would make ticket 17's MLX gate the most expensive job in CI.
Public makes both free.

**Prerequisite:** scan the history for anything that should not be public
before flipping.

### 2. Version: `v0.1.0`

Relax the workflow regex to accept stable SemVer alongside prereleases (today it
*rejects* anything without a prerelease suffix).

`0.x` is SemVer's device for "usable, interface may still break" — which is
true: ticket 01 breaks gateway read auth and ticket 04 changes the proto. A
`v1.0.0` would promise wire stability across ~35 RPCs immediately after changing
several of them. Bump to 1.0.0 once the contracts have held for a few releases.

### 3. Signing: build provenance only

GitHub's keyless build-provenance attestation. The signer identity is the
workflow; users verify with `gh attestation verify`. `SHA256SUMS` stays for
offline integrity.

Rejected: a separate cosign signature (a second artifact proving roughly the
same thing, with a second verification path to document) and a maintainer GPG
key (key custody, rotation and revocation for a solo maintainer).

Ticket 13 pins the exact action version and verification syntax — do not write
either from memory.

### 4. Channels: read the existing release list

- `stable` — the latest **non**-prerelease (GitHub's `releases/latest` already
  excludes prereleases).
- `dev` — the most recent release **including** prereleases.

Both resolve to an immutable tag before download, satisfying ticket 15's
"never install from a moving reference". No new publishing infrastructure: the
existing manual-dispatch workflow already produces both.

Rejected: a rolling nightly build of `main` — needs a nightly job, a mutable
tag, and a story for when `main` is red.

### 5. Release gates: every automated check, including MLX

Extend the existing check-run wait loop to require, all green on the release
SHA: `Test`, `Lint`, coverage floor, llama.cpp E2E, browser E2E,
packaged-artifact E2E (ticket 16) and MLX (ticket 17).

MLX is **required**, not advisory: shipping a `darwin/arm64` artifact *is* a
claim of MLX support, and the support matrix must not claim more than the
evidence. The release job dispatches the MLX workflow on the release SHA and
waits, the same way it waits for `Test` today.

Manual QA sign-off is not a CI check and does not need to be — the release is
`workflow_dispatch`-only, so **the human dispatching it is the sign-off**, with
tickets 11 and 18 as the recorded evidence.

### 6. Licence: nothing Apache-derived

Checked: "Apache" appears only in `docs/feature-roadmap.md` and this ticket. No
ODS reference exists in any source file. The roadmap's Apache/MIT concern is
unfounded as written.

What does exist is attribution comments citing the upstream references, e.g.
`backends/llamacpp/backend.go:9` ("Ported and neutralized from llamarig").
**Open item:** confirm what licence `llamarig` and `mlxrig` carry. If they are
the maintainer's own and MIT-compatible, nothing to do beyond the comments
already present.

### Consequences

- Ticket 14 **shrinks**: version stamping, checksums, smoke test and the
  check-run wait already exist. Its real work is three build targets instead of
  one, the provenance step, the regex relaxation, and the longer gate list.
- Ticket 15 depends on the repo being public.
- Ticket 17's MLX job becomes a hard release blocker.
