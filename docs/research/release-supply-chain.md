# Release supply-chain research

Verified 2026-08-01 for ticket 13. Version numbers below are deliberately
pinned to releases that existed on that date. Ticket 12 has already selected
GitHub build-provenance attestations, not a separate cosign signature; the
cosign section records the requested facts without reopening that decision.

## 1. Cross-compilation

The module has no SQLite dependency and no direct dependency that requires
cgo (`go.mod`). The TUI is Bubble Tea, and the control transport uses Go's Unix
socket support. Go's build documentation defines `GOOS`, `GOARCH`, and
`CGO_ENABLED`; when cgo is disabled, files importing `C` are excluded from the
build ([Go build constraints](https://pkg.go.dev/cmd/go#hdr-Build_constraints),
[cgo documentation](https://pkg.go.dev/cmd/cgo)).

The repository was tested after building the embedded web assets in the same
order as `.github/workflows/release.yml`:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o inferencerig-linux-amd64 .
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o inferencerig-linux-arm64 .
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o inferencerig-darwin-arm64 .
```

All three commands succeeded. `file` identified the results as an x86-64 ELF
binary, an arm64 ELF binary, and an arm64 Mach-O binary respectively; both ELF
binaries were statically linked. The only initial failure without the web
build was Go's `embed` check for the missing `webui/dist`, not cgo.

**Recommendation.** Keep the existing web build, then use a three-entry
`GOOS`/`GOARCH` matrix with `CGO_ENABLED=0`. No C cross-toolchain and no release
framework are needed for compilation.

## 2. Release automation

The existing workflow already validates a version, waits for checks, builds
the web UI, stamps build information, packages LICENSE and README, makes
`SHA256SUMS`, smoke-tests an extracted binary, and invokes
`gh release create`. GitHub CLI documents that `gh release create` accepts
asset paths, targets a commit, generates notes, and can mark a release as a
prerelease ([GitHub CLI manual](https://cli.github.com/manual/gh_release_create)).
GitHub Actions' matrix strategy is the native mechanism for running a job for
several variable combinations
([matrix documentation](https://docs.github.com/actions/using-jobs/using-a-matrix-for-your-jobs)).

GoReleaser can also cross-compile, archive, checksum, sign, create SBOMs, and
publish, but that would duplicate working repository logic and introduce its
own configuration and version pin
([GoReleaser documentation](https://goreleaser.com/customization/)).

**Recommendation.** Extend the current Actions workflow with the three build
targets, SBOM files, and provenance step, then continue publishing with
`gh release create`. GoReleaser does not make ticket 14 smaller enough to
justify another tool or configuration owner.

## 3. Checksums and keyless cosign signing

Checksums remain the offline integrity mechanism. Generate one manifest after
all archives exist:

```sh
sha256sum ./*.tar.gz > SHA256SUMS
```

For the separately requested cosign facts, the official installer's current
release was **v4.1.2**, so the current action major is
`sigstore/cosign-installer@v4`
([official release](https://github.com/sigstore/cosign-installer/releases/tag/v4.1.2)).
GitHub OIDC keyless signing needs `id-token: write`; workflows should retain
only the additional permissions their checkout and release steps need, such as
`contents: read` while signing
([Sigstore CI quickstart](https://docs.sigstore.dev/quickstart/quickstart-ci/),
[GitHub OIDC permissions](https://docs.github.com/actions/security-for-github-actions/security-hardening-your-deployments/about-security-hardening-with-openid-connect#adding-permissions-settings)).

An action step and signing command would be:

```yaml
permissions:
  contents: read
  id-token: write

- uses: sigstore/cosign-installer@v4
- run: cosign sign-blob --yes --bundle artifact.sigstore.json artifact.tar.gz
```

Cosign requires verification to constrain both the Fulcio certificate's OIDC
issuer and its workflow identity. For a release workflow run from `main`, the
exact user command is:

```sh
cosign verify-blob artifact.tar.gz \
  --bundle artifact.sigstore.json \
  --certificate-identity "https://github.com/rivt-ai/inference-rig/.github/workflows/release.yml@refs/heads/main" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com"
```

The bundle form and identity/issuer checks are documented by
[Sigstore's blob verification guide](https://docs.sigstore.dev/cosign/verifying/verify/).

**Recommendation.** Keep `SHA256SUMS`, but do not add cosign: ticket 12 chose
GitHub's provenance attestation as the single keyless verification path.
Cosign would add a second bundle, installer, and user command for substantially
overlapping evidence.

## 4. GitHub build provenance

The current release of the official action was **v4.1.1**, so use
`actions/attest-build-provenance@v4`
([official release](https://github.com/actions/attest-build-provenance/releases/tag/v4.1.1)).
GitHub's documented permissions for a build attestation are:

```yaml
permissions:
  id-token: write
  contents: read
  attestations: write
```

The action takes `subject-path` (or a supplied digest), creates a signed SLSA
build-provenance attestation for that subject, and writes it to GitHub's
attestation store
([action README](https://github.com/actions/attest-build-provenance/tree/v4.1.1),
[GitHub artifact-attestation documentation](https://docs.github.com/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations)).
Attest the release archives and the checksum/SBOM files, after their bytes are
final and before publishing them.

A user verifies a downloaded subject against this repository with:

```sh
gh attestation verify artifact.tar.gz --repo rivt-ai/inference-rig
```

The repository binding is to the name the release was built under, which this
repository's rename from `rivt-ai/InferenceRig` splits in two: releases up to
v0.3.1 verify only against the old name, later ones only against the new.

GitHub documents the same command and repository binding
([GitHub verification documentation](https://docs.github.com/actions/how-tos/secure-your-work/use-artifact-attestations/verify-attestations)).

**Recommendation.** Add `actions/attest-build-provenance@v4` with the three
permissions above and attest every published file. This implements ticket
12's selected signing identity without a maintained key or another verifier.

## 5. SBOM

`go version -m <binary>` prints the build information embedded by the Go
linker, including the main module, dependency modules, build settings, and
versions. The Go command also exposes that information as JSON
([Go `version -m` documentation](https://pkg.go.dev/cmd/go#hdr-Print_Go_version)).
It is useful evidence, but it is not a CycloneDX or SPDX document and therefore
does not by itself meet the SBOM requirement.

Two standard-format generators work:

- Syft **v1.50.0** can catalog a built filesystem or binary and emit SPDX or
  CycloneDX ([official release](https://github.com/anchore/syft/releases/tag/v1.50.0),
  [output formats](https://github.com/anchore/syft/wiki/Output-Formats)).
- `cyclonedx-gomod` **v1.10.0** has a `bin` command specifically for creating a
  CycloneDX SBOM from a Go binary and its embedded module information
  ([official release](https://github.com/CycloneDX/cyclonedx-gomod/releases/tag/v1.10.0),
  [official command documentation](https://github.com/CycloneDX/cyclonedx-gomod/tree/v1.10.0#cyclonedx-gomod-bin)).

A pinned, artifact-specific invocation is:

```sh
go install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.10.0
cyclonedx-gomod bin -json -output inferencerig.cdx.json ./inferencerig
```

Run it once for each target binary before archiving, and publish the resulting
SBOM beside that archive. The generator is a release-workflow tool, not a
runtime or module dependency.

**Recommendation.** Use pinned `cyclonedx-gomod` because it is the narrowest
tool that actually emits a standard SBOM for the built Go artifact. Retain
`go version -m` as a smoke/inspection command if useful; do not label its
output an SBOM. Syft is preferable only if future releases need one scanner
for non-Go files or container images too.

## 6. macOS Gatekeeper

Gatekeeper verifies downloaded software from outside the App Store and, for
software carrying quarantine metadata, checks that it is from an identified
developer, notarized, and unmodified
([Apple Platform Security](https://support.apple.com/guide/security/gatekeeper-and-runtime-protection-sec5599b66df/web)).
Browsers participate in macOS's file-quarantine mechanism, so a downloaded
unsigned binary is assessed on first launch and the user receives the
unidentified/developer warning. A plain command-line `curl -o` writes the HTTP
response bytes and normally does not attach the browser's quarantine extended
attribute. Consequently that path normally avoids the first-launch Gatekeeper
assessment. This difference is metadata, not a stronger property of the
binary; an installer can check it with `xattr -p com.apple.quarantine FILE`.
Apple documents `xattr` as the interface for inspecting and changing extended
attributes
([Apple `xattr(1)` manual](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man1/xattr.1.html)).

An ad-hoc signature records code-directory hashes but has no trusted Developer
ID identity, so it does not turn a quarantined download into identified and
notarized software. Apple's outside-App-Store distribution path is Developer
ID signing followed by notarization; notarization scans the submission and
issues a ticket that can be stapled to the distributed software
([Apple notarization workflow](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution-custom-workflow),
[Apple code-signing overview](https://developer.apple.com/support/code-signing/)).

**Recommendation.** For the first `curl | sh` release, document that the
binary is unsigned/unnotarized and verify its checksum plus GitHub attestation;
do not add or strip quarantine metadata in the installer and do not pretend
ad-hoc signing solves trust. Browser/manual downloads may require an explicit
user override. Full Developer ID signing and notarization is the durable fix
when friction for browser downloads justifies Apple credential custody and a
macOS signing job.
