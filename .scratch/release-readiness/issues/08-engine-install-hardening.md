# 08 — Harden engine installation and rollback

Type: task
Status: open
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
