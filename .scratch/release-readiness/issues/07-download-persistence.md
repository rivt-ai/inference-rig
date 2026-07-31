# 07 — Persist downloads and recover partial artifacts

Type: task
Status: claimed
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
