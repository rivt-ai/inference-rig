# 14 — Multi-platform build and signed GitHub Release

Type: task
Status: open
Blocked by: 11, 12, 13
Milestone: B
Roadmap: P0 #4

## Question

Turn a tag into a published, verifiable release.

Read ticket 12's decisions and ticket 13's findings first — versions, signing
identity and tool choice come from there, not from memory.

Do, extending `.github/workflows/release.yml` rather than adding a parallel
workflow:

- Build Linux amd64, Linux arm64 and macOS arm64 artifacts, with the web UI
  assets embedded (`make webui` is a prerequisite of the binary).
- Stamp version, commit and build date into the binary; `inferencerig
  --version` reports them.
- Produce per-artifact SHA-256 checksums, an SBOM, build provenance and
  signatures per ticket 13.
- Attach everything to a GitHub Release, with release notes that name the
  breaking changes from Milestone A (no users, but the notes must still say
  what changed).
- Gate the release on the checks ticket 12 marked as hard blockers.
- Document the verification commands a user runs, in the README.

Acceptance:

- A tag produces all three artifacts plus checksums, SBOM, provenance and
  signatures.
- The documented verification commands succeed against a real published
  artifact (a pre-release tag is fine for proving this).
- Each artifact runs `--version` on its target platform — Linux both
  architectures in CI; macOS arm64 verified by the `macos-15` runner.
- A failing hard-blocker check prevents publication.
- `make test` and `make lint` green.
