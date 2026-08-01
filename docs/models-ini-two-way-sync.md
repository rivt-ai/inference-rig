# Future: two-way `models.ini` synchronization

InferenceRig currently treats YAML profiles as canonical and materializes
`generated/llamacpp/models.ini` before starting the llama.cpp runtime. Users may
edit that INI file directly, but the next materialization overwrites those
changes.

A future change may synchronize supported manual INI edits back into the
canonical YAML profiles before regeneration. It should:

- detect changes since the last materialization;
- import known model and engine-argument fields without losing YAML-only data;
- define deterministic conflict handling when both YAML and INI changed;
- reject invalid or ambiguous edits without replacing either valid file; and
- write both formats atomically enough to avoid partial synchronization.

Until those rules exist, the generated file's header must continue to state
that manual edits are temporary.
