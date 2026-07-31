# 03 — Decide backend concurrency semantics

Type: grilling
Status: open
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
