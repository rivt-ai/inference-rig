# 08 — Harden engine installation and rollback

Type: task
Status: resolved
Blocked by: none
Milestone: A
Roadmap: P1 #9

## Question

Make installing an inference engine verifiable and reversible.

Owner: the managed installers behind `backends/llamacpp` and `backends/mlx`
(`Backend.Install`). Keep engine specifics inside those packages — the neutral
layer only learns the record format.

Do:

- Preserve llama.cpp's existing digest and size validation.
- Validate every archive entry before extraction: reject absolute paths, `..`
  traversal, symlinks escaping the root, and unexpected file modes.
- Pin MLX packages with hashes or a locked artifact set (no unpinned
  `pip install` at runtime).
- Record for every installation: source, version, digest, platform,
  accelerator, install time. One neutral record shape, stored under
  `${INFERENCERIG_HOME}`.
- Verify a staged binary before activating it (execute a version probe).
- Make rollback a real operation — `backend rollback` restoring the previous
  recorded installation — not just "the old files happen to still be there".

Acceptance:

- A crafted archive with a `../` entry is rejected without writing outside the
  target.
- A staged binary that fails its probe is never activated.
- Rollback returns the previously recorded version and the record reflects it.
- The install record is what the release receipt (ticket 16) and the future
  `doctor` will read — so it must be machine-readable.
- `make test` and `make lint` green.

## Answer

Installs are now verified, recorded and reversible. What later tickets depend on:

**The install record (ticket 16's receipt, the future `doctor`).**
`backends.InstallRecord` in `backends/installstate.go` is the one neutral shape:
backend, version, source, digest, platform, accelerator, directory, executable,
installed_at. Two of them live in `backends.InstallState` (`active` + `previous`)
as JSON at `${INFERENCERIG_HOME}/engine/<backend>/state.json`, mode 0600. Read it
with `backends.ReadInstallState(backends.EngineRoot(name))` — `EngineRoot` is now
shared instead of resolved separately inside each backend. This replaced both
backends' bespoke state shapes and the generic `ReadInstallState[T]`, so there is
exactly one format to parse.

**Archive validation.** `backends/llamacpp/archive.go` extracts with
`archive/tar` + `compress/gzip` instead of shelling out to `tar(1)`. Every entry
is checked before anything is written: `filepath.IsLocal` rejects absolute paths
and traversal, link targets are resolved and rejected if they leave the
destination, and only dir/regular/link types with plain permission bits are
accepted (no device, fifo, setuid, setgid or sticky). A rejected entry fails the
whole extraction. `io.CopyN` bounds each write by the declared entry size. The
digest+size check on the release asset is unchanged and now also lands in the
record as `sha256:...`.

**Probe before activation.** `probeExecutable` runs `<binary> --version` (30s
timeout) on the *staged* payload, before the rename into the managed location, so
a payload that fails is discarded with its staging directory and the working
install stays active. MLX probes `import mlx_lm, mlx_lm.server` before writing
its record.

**MLX pinning.** `backends/mlx/requirements.lock` is the complete transitive set
(embedded via `go:embed`), installed with `pip install --no-deps -r`, so pip
resolves nothing at runtime. Only `ManagedVersion` is installable — any other
requested version fails with `ErrUnlockedVersion` rather than falling back to an
unpinned resolve. Environments are version-scoped (`venv-<version>/`) with the
same active+previous retention as llama.cpp, which is what makes MLX rollback a
state swap instead of a re-download. Regeneration command is in the lock's
header; bumping `ManagedVersion` means regenerating it. Version pins, not
hashes — hashes pin a wheel to one interpreter/platform tag and would break every
other managed host (noted as a `ponytail:` ceiling in the lock).

**Rollback.** `Backend.Rollback(ctx)` is on the contract interface (not an
optional facet — both managed backends have it, and the contract suite asserts a
single install rolls back to `backends.ErrNoPreviousInstall` rather than
silently succeeding). The shared implementation is `backends.RollbackInstall`:
verify the previous record with a backend-supplied probe, then swap active and
previous, so a rollback is itself reversible and nothing is written when the
probe fails. Exposed as `RollbackBackend` (answering `InstallBackendResponse` —
a rollback reports what an install reports) and `inferencerig backend rollback
<backend>`; it is in the gateway's `mutatingProcedures` set.

Not done, deliberately: no web UI or MCP surface for rollback (CLI + RPC is what
the ticket asked for), and no `installs.json` history beyond active+previous —
retention already keeps exactly two.
