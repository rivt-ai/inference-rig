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
