# 03 — Decide backend concurrency semantics

Type: grilling
Status: resolved
Blocked by: none
Milestone: A
Roadmap: P2 #13

## Question

What concurrency policy does a backend declare, and how do runtime slots,
validation, autostart and the UIs derive their behaviour from it?

The `backends.Backend` contract exposes capabilities, but `core/control`
currently keeps one runtime slot per backend — an assumption, not a declared
policy. Tickets 04, 05 and 06 all need this settled first: the state machine's
slot identity, reconciliation's "which process belongs to which profile", and
autostart's conflict rules all fall out of it.

Model explicitly which of these a backend is:

- one process, one active profile;
- one router process with several loaded profiles (llama.cpp's router — note
  the existing optional `RuntimeActivator` facet in `backends/contract.go`);
- several independent processes;
- multi-model routing inside one process.

Decide and record:

1. How the policy is declared — an addition to `Capabilities`, or a new method?
   Keep it inside the existing contract if at all possible.
2. What the slot key becomes (backend name today; profile name? profile+port?).
3. What happens when a second profile targets a single-active backend — reject
   at validation, or stop-then-start with an explicit confirmation?
4. What autostart does with an impossible combination (ticket 06 consumes this).
5. What the TUI and web UI show for each policy.

Use `/grilling` and `/domain-modeling`. Read `backends/contract.go`,
`backends/registry.go`, `core/control/manager.go` and `core/runtime/supervisor.go`
first.

The answer is the policy, the contract change it implies, and the term for it
in `CONTEXT.md`. Implementation lands in ticket 04.

## Answer

Resolved 2026-07-31 with the effort owner. This is the spec for ticket 04 —
implement it, do not re-litigate it.

### The actual defect (verified, not assumed)

A concurrency signal already exists and is already correct: `SingleActiveProfile`
(`backends/types.go:164`). MLX declares `true` (`backends/mlx/backend.go:85`);
llama.cpp leaves it `false` (`backends/llamacpp/backend.go:93`) and implements
`RuntimeActivator` instead (`backends/llamacpp/activate.go:36`).

**The manager ignores it.** `runtimes` is keyed by backend name
(`core/control/manager.go:84`) and `StartRuntime` unconditionally stops whatever
occupies that key before starting (`manager.go:278-282`). So llama.cpp's router
— launched with every profile as a preset — is killed and cold-restarted on
every profile switch, paying a full model load, while the capability that says
it needn't be is computed, shipped over the wire (`control_service.go:641`), and
never acted on.

Also relevant: `backends/all/register.go` registers MLX only on `darwin/arm64`.
On Linux, llama.cpp is the only backend that exists, so everything below is a
no-op there and only does real work on Apple Silicon.

### 1. No new contract surface

`SingleActiveProfile` + presence of `RuntimeActivator` fully determine slot
behaviour for both real backends:

- **exclusive** — `SingleActiveProfile: true` (MLX). One profile per process;
  switching means stop-then-start.
- **router** — `SingleActiveProfile: false` + implements `RuntimeActivator`
  (llama.cpp). One process holds several profiles; switching means
  `ActivateRuntime`.

Rejected: a `ConcurrencyPolicy` enum. The roadmap's other two modes ("several
independent processes", "multi-model routing inside one process") have no
implementation and no requester. If a third backend ever needs one, replacing
the two booleans with an enum is a single commit.

**Registry guard:** `Registry.Register` must reject a backend that declares
`SingleActiveProfile: false` and does not implement `RuntimeActivator` — that
combination has no defined behaviour under this model, and the gap must not
reach the manager.

### 2. One backend active at a time, globally

InferenceRig serves one backend at a time. Switching backends requires an
explicit reset.

- The manager tracks `activeBackend`: set when the first profile starts,
  cleared when the last stops.
- Starting a profile whose `Profile.Backend` (`core/profiles/profile.go:18`)
  differs from `activeBackend` returns a typed conflict naming the active one.
- A new **reset** operation stops every runtime and clears `activeBackend`.

Reset is *not* a daemon restart. The daemon holds no engine state of its own,
so a restart buys no isolation that stopping the processes doesn't, and it
would take the gateway and TUI down with it.

### 3. Slot model

At most one `runtimeSlot{process, profiles}`, still keyed by backend name — the
key PID files, ports and reconciliation already use.

- Exclusive backend: the profile set holds exactly one name.
- Router backend: the set holds every activated profile in that one process.
- Stopping the last profile stops the process; stopping one of several just
  deactivates it.

Rejected: keying slots by profile name. Two profiles map to one OS process, so
every stop and delete path would have to work out whether it owns the process,
and reconciliation would have to rebuild the sharing from PID files. That moves
the complexity rather than removing it.

### 4. No implicit kills

Starting Y while X runs on an exclusive backend returns a typed conflict naming
X. A `replace` field on `StartRuntimeRequest` performs the stop-then-start.
CLI gets `--replace`; TUI and web confirm first, reusing their
destructive-action pattern.

This matches the cross-backend reset rule: after this ticket, **no client —
including MCP — can terminate a running engine without saying so.**

### 5. Isolation is visible, not hidden

All profiles stay listed. Profiles belonging to a non-active backend render
dimmed and unstartable, with the reason ("MLX is active — reset to start
llama.cpp profiles") and the reset action offered inline.

Rejected: filtering them out. A user who cannot find a profile they created
will believe it was deleted.

`Info` (`core/control/operations.go:23`) gains `ActiveBackend` so every front
end renders the same state.

### 6. Consequences for other tickets

- **Ticket 04** owns the implementation, including the proto changes: `replace`
  on `StartRuntimeRequest`, `active_backend` on the info message, and one new
  reset RPC.
- **Ticket 05 (recovery):** if reconciliation finds live adopted processes from
  *two* backends, the invariant above was violated. Classify both as orphaned
  and require an explicit reset — never silently pick a winner.
- **Ticket 06 (autostart):** autostart profiles must all name one backend; a
  mix is rejected at **config validation**, not at boot. More than one
  autostart profile on an exclusive backend is likewise a validation error.
  Several on a router backend is fine and all of them activate.
- Add the terms **exclusive backend**, **router backend**, **active backend**
  and **runtime slot** to `CONTEXT.md` when ticket 04 lands.
