# Claiming a ticket

How an agent picks up parallelizable work without colliding with other agents
working the same map at the same time.

Active map: `.scratch/release-readiness/map.md`. Read it first — its `## Notes`
section carries the standing rules for the effort, and `## Decisions so far`
tells you what has already been settled so you don't reopen it.

## 1. Find an unclaimed ticket

A ticket in `.scratch/release-readiness/issues/` is **takeable** when all three
hold:

- `Status: open` — not `claimed`, not `resolved`;
- every ticket in its `Blocked by:` line is `Status: resolved`;
- its `Type:` is `task` or `research`.

Lowest number wins. Always `git pull` first — a claim you haven't fetched looks
like an opening.

**Do not claim** `Type: grilling`, or a task marked `(HITL)`. Those resolve only
through live conversation with the effort owner, and an agent that answers its
own questions has produced a decision nobody made.

## 2. Claim it before doing anything

The claim is a commit, not an intention. An uncommitted edit is invisible to
every other agent.

```sh
git switch phase-01-bootstrap && git pull
# set Status: claimed in the ticket file
git commit -am "chore: claim ticket NN" && git push
```

Push it **before** you start work. This is the whole race-avoidance mechanism:
first push wins, and a second agent that pulls sees `claimed` and moves on. If
your push is rejected because someone else claimed it first, pick another
ticket.

## 3. Branch from `phase-01-bootstrap`

```sh
git switch -c ticket/NN-<slug> phase-01-bootstrap
```

**Every branch and every PR targets `phase-01-bootstrap`. Never `main`.**
`main` stays untouched until the effort's final ticket merges the integration
branch in one go.

## 4. Do the work

- Invoke `/ponytail` and keep it the laziest change that works. Simplicity is
  the standing instruction for this effort.
- Read `AGENTS.md` and `docs/architecture-overview.md` before touching code.
- If the ticket says "read ticket NN's `## Answer` first", that answer is a
  settled decision — implement it, don't relitigate it.
- One ticket per session. If you finish early, stop; don't drift into the next
  one, because someone else may already have claimed it.
- Commit small and often. `make test` and `make lint` must pass before you
  push. Never weaken or skip a test to make the gate green.

## 5. Resolve it

- Append what you did and anything later tickets depend on under `## Answer` in
  the ticket file.
- Set `Status: resolved`.
- Append one line to the map's `## Decisions so far`: the gist plus a link.
- Open a PR into `phase-01-bootstrap`.

## If you can't finish

Set `Status: open` again, push that, and say why in the ticket. An abandoned
`claimed` ticket is worse than an open one — it's invisible to the frontier and
nobody knows it's dead.

## Prompt to hand an agent

> Read `docs/agents/claiming-tickets.md`. Pull, take the lowest-numbered
> takeable ticket from `.scratch/release-readiness/issues/`, claim it with a
> pushed commit before starting, branch from `phase-01-bootstrap`, and resolve
> exactly that one ticket. Use `/ponytail`. Target `phase-01-bootstrap` with
> your PR, never `main`.
