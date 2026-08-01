# 07 — Persist downloads and recover partial artifacts

Type: task
Status: resolved
Blocked by: none
Milestone: A
Roadmap: P1 #7

## Question

Make model downloads survive a restart and prove what they fetched.

Owner package is `core/modeldownload/` (already at ~82% coverage — extend it,
do not add a parallel downloader).

Do:

- Persist job metadata atomically (write-temp-then-rename).
- Reconcile `.part` files at startup: resume, restart or discard, each decided
  explicitly and logged.
- Resume only when the server proves safe range support; otherwise restart the
  download.
- Verify SHA-256 (or stronger) from catalog metadata before a file is accepted.
- Resolve mutable repository references to an immutable revision **before**
  downloading, and record the revision.
- Enforce maximum size, redirect count, hostname and protocol policy.

Acceptance:

- Killing the daemon mid-download and restarting either resumes or cleanly
  restarts, never leaving a corrupt file presented as complete.
- A digest mismatch fails the job and removes the artifact.
- A redirect to a non-allowed host is refused.
- Tests cover resume, no-range-support restart, digest mismatch, size cap and
  redirect policy.
- `make test` and `make lint` green.

## Answer

All of it landed in `core/modeldownload/` (extended, not duplicated), plus the
`backends` plan types it needed. What later tickets can rely on:

- **Persistence.** `Options.StateDir` turns on one JSON record per job
  (`{job, request}`) written with `filedoc.AtomicCreate` — temp-then-rename,
  already the repo's atomic writer. Records are written on transitions only
  (queue, running, terminal), never per progress tick; the `.part` file on disk
  is the byte-level progress. Location is `config.Paths.DownloadState`
  (`<home>/state/downloads`), wired in `bootstrap.NewService`.
- **Reconciliation.** `Manager.Recover(ctx)` runs at daemon start and decides
  per record, logging each: interrupted (`queued`/`running`) → re-queued;
  target already landed → completed and the stale `.part` discarded; job
  already finished → `.part` discarded; unreadable record → removed. Restored
  records also make `Get` work for jobs from a previous process.
- **Resume vs restart.** A transfer with a `.part` sends `Range: bytes=N-` and
  only resumes when the response is `206` *and* `Content-Range` starts at N;
  any other 2xx truncates and restarts. Partials deliberately survive a failed
  job so the next attempt can continue — `prepareParent` no longer deletes the
  stage file, which is what previously made resume impossible.
- **Digest.** `ArtifactItem.SHA256` is verified before the artifact is
  finalized, and a mismatch removes the `.part`, so a corrupt file can neither
  be published nor resumed into permanently. Populated today from Hugging Face
  `lfs.oid` for MLX snapshots; empty digest = unverifiable, not a failure.
  Verification re-reads the file (marked `ponytail:` at the call site) —
  incremental hash state would have to survive both a resume and a restart.
- **Revision pinning.** MLX resolution reads the repository commit from the
  API call it already makes and pins every file URI to `/resolve/<sha>/`,
  recording it in `ResolvedModel.Metadata["revision"]` →
  `ArtifactPlan.Revision` → `Job.Revision`. This is where straddling matters: a
  snapshot is many requests. **Not done for llama.cpp**, whose `Resolve` is
  documented as network-free and would need a new HTTP dependency to pin its
  single-file `/resolve/main/` URI; its transfer is one request, so it cannot
  straddle two revisions. Ticket 08 or a follow-up should decide whether
  offline planning or pinning wins there.
- **Transfer policy.** `Options.MaxBytes` (default 512 GiB), `MaxRedirects`
  (default 5) and `AllowedHosts` (empty = any host). Scheme is http/https only.
  The policy is applied to the first request *and* every redirect via
  `CheckRedirect` on a copy of the injected client, so a redirect off the
  allowlist is refused without the transport being swapped out.

Tests: `core/modeldownload/persist_test.go` covers resume, no-range restart,
digest mismatch, size cap, disallowed redirect host, and a simulated
kill-and-restart through `Recover`. `make test` and `make lint` green (`make
lint` needs `make webui` first — the embed pattern fails on a bare clone).
