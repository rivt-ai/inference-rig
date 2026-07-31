# 13 — Research: release supply-chain toolchain

Type: research
Status: open
Blocked by: none
Milestone: B
Roadmap: P0 #4

## Question

Establish the current, verified facts needed to build a signed multi-platform
Go release on GitHub Actions — from primary sources, not memory. Pinned
versions in this repo's docs must not be invented.

Answer, with a citation for each:

1. **Cross-compilation** — for `linux/amd64`, `linux/arm64`, `darwin/arm64`:
   what does this repo's build actually need? Check whether cgo is required
   (the TUI, the Unix socket transport, and any sqlite-like dependency) by
   inspecting `go.mod` and building with `CGO_ENABLED=0`. If pure Go works,
   plain `GOOS`/`GOARCH` builds are the lazy answer and no cross-toolchain or
   release tool is needed.
2. **Release automation** — is a tool (GoReleaser or similar) worth it here, or
   does a `go build` matrix plus `gh release create` cover the requirement?
   Recommend the smaller one that meets ticket 14's acceptance criteria.
3. **Checksums and signing** — the current documented way to sign release
   artifacts with keyless GitHub OIDC (cosign / sigstore): action name, current
   major version, required workflow permissions, and the exact verification
   command a user runs.
4. **Provenance** — GitHub's build-provenance attestation action: current
   version, permissions, what it attests, and how a user verifies it with `gh`.
5. **SBOM** — how to generate a CycloneDX or SPDX SBOM for a Go module
   (`go version -m`, syft, or the Go toolchain's own support), and which is
   least dependency-heavy.
6. **macOS specifics** — what an unsigned/unnotarized macOS arm64 binary does
   on Gatekeeper when downloaded via curl versus a browser, and what the
   minimum viable answer is (quarantine attribute, ad-hoc signing, full
   notarization). This decides how much macOS pain ticket 15's install script
   must absorb.

Deliverable: a Markdown file under `docs/research/` with sources and version
numbers, plus a one-paragraph recommendation per item. Facts only — the
decisions are ticket 12's and ticket 14's.
