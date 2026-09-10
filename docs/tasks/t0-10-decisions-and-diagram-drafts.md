# T0.10 DECISIONS.md and first diagram drafts

**Item:** M11 | **Milestone:** MS0 | **Week:** 2
**Suggested owner:** Vivek for diagrams, Vighnesh for decisions | **Reviewer:** Ritk
**Effort:** 3 hours
**Depends on:** nothing
**Unblocks:** T11.03

## Goal

Every Phase 0 technical decision is written down while the reasons are fresh, and the two diagrams that the interface freeze depends on exist as editable Graphviz sources.

## Context you need

MASTER-PLAN Section 3 lists the decisions. The existing `docs/figures/fig5_1.png` and `fig5_5.png` are the synopsis versions and are the starting point.

## Steps

1. Create `docs/DECISIONS.md`. One entry per decision with four labelled lines: Context, Decision, Rejected, Consequence. Entries: raw epoll via `syscall`; three background goroutines and why they need no lock; timers inside the loop; injected clock; expiry deletions not propagated; snapshot by clone plus copy-on-write; compaction triggered by BGSAVE not by command.
2. Create `docs/diagrams/architecture.dot` reproducing Fig. 5.1 with the actual package names from the repo added in a second label line per box.
3. Create `docs/diagrams/request-lifecycle.dot`: socket read, parse, dispatch, handler, store mutation, Propagate, AOF append, reply serialise, write.
4. Add a `Makefile` target `diagrams` that runs `dot -Tpng` into `docs/diagrams/out/`.
5. Commit the sources and the rendered PNGs.

## Acceptance

- [ ] `make diagrams` renders both without warnings.
- [ ] Each decision entry can be read aloud in under a minute and answers "why not the alternative".
