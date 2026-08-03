# Context: InferenceRig

A neutral control plane for local inference engines. Engine specifics live only
behind `backends.Backend`; everything below is vocabulary the neutral core uses,
and code, tests and issues should use these words rather than synonyms.

See `docs/architecture-overview.md` for the code map and `AGENTS.md` for the
rules that constrain changes to it.

## Glossary

**Runtime slot** — the one runtime the control manager owns: a process, the set
of profiles it serves, and an explicit state (`stopped`, `reconciling`,
`starting`, `activating`, `running`, `stopping`, `failed`, `orphaned`). The
state is authoritative; it is never inferred from whether a process object
exists or from which profiles are listed. Owned by `core/control/slot.go`.

**Exclusive backend** — a backend whose `Capabilities.SingleActiveProfile` is
true: one process serves one profile, so switching profiles means stopping the
running one first. Apple MLX is one.

**Router backend** — a backend whose `SingleActiveProfile` is false and which
implements `backends.RuntimeActivator`: one process holds several profiles and
is told which to serve. llama.cpp's router is one. A backend that is neither is
rejected at registration, because its slot behaviour is undefined.

**Active backend** — the backend the runtime slot belongs to. InferenceRig
serves one backend at a time: while a slot exists, a profile naming any other
backend cannot start, and a **reset** — stopping every runtime and clearing the
slot — is what switches. A reset is not a daemon restart; the daemon holds no
engine state of its own.

**Replace** — the caller stating it accepts the running profile being stopped
(`--replace`, `StartRuntimeRequest.replace`). Without it, starting a second
profile on an exclusive backend is a conflict: no client, including MCP, can
terminate a running engine without saying so.

**Operation ID** — the identifier tying every state transition of one start,
stop or reset together. It appears on each transition in the event stream and
audit log, so a client follows a lifecycle by watching events instead of polling
status.

## Gateway security model

Every RPC and `/mcp` is authenticated by default, `/health` and the static app
shell excepted; the guard wraps the Connect *handler*, not a unary interceptor,
so the three server streams (`WatchEvents`, `WatchLogs`, `WatchModelCatalog`)
are covered too. There is no login system: a bearer token persists to
`run/gateway.token` and is delivered to the browser via a `#token=` fragment on
the printed launch URL, which never reaches a server log. `security.disable_auth`
on a non-loopback bind is a load error unless `security.allow_exposed_without_auth`
is also set; auth posture is always shown — startup log, `/health` JSON, TUI
badge, and a web banner shown only on a non-loopback bind. `allowed_origins` is
a list replacing the loopback default (`security.disable_origin_check` is the
narrower opt-out for a proxy that terminates origin itself). Credential-shaped
argv is redacted from `command.Display`; paths and other argv are not.

## Release identity and channels

No signing key and no cosign: GitHub's keyless `actions/attest-build-provenance`
is the one verification path (`gh attestation verify <artifact> --repo
antonikliment/InferenceRig`), alongside a CycloneDX SBOM per binary
(`cyclonedx-gomod bin`) and `SHA256SUMS`. Releases are plain GitHub Releases,
not a package manager; `stable` (install script default) resolves GitHub's own
latest-non-prerelease via `/releases/latest`, `dev` takes the newest release of
any kind — both resolve to an immutable tag before downloading, never a moving
reference. A release publishes four targets (linux/amd64, linux/arm64,
darwin/amd64, darwin/arm64); publication is gated on `Test`, `Lint`, the MLX
inference job (`.github/workflows/mlx.yml`, invoked as `workflow_call`) and a
real E2E pass against the packaged linux/amd64 tarball — a failing gate blocks
`gh release create` outright. macOS binaries are ad-hoc built, not Developer ID
signed or notarized; see `docs/research/release-supply-chain.md` for why that
gap is intentional for now. There is no separate machine-readable release
receipt — the linked CI runs are the evidence trail (see
`docs/hardware-validation.md`).
