# T11.03 Final diagrams

**Item:** M11 | **Milestone:** MS11 | **Week:** 14
**Suggested owner:** Vivek | **Reviewer:** Vighnesh
**Effort:** 2 to 3 hours
**Depends on:** T0.10, T9.09 if S1 survived
**Unblocks:** T11.04

## Steps

1. Update `architecture.dot` and `request-lifecycle.dot` from T0.10 to match the final code (package names, the eventfd path, the fsync goroutine).
2. Add `persistence-flow.dot`: write path through dispatcher, Propagate, AOF append, fsync policy; `BGSAVE` clone, temp file, rename; startup recovery sequence.
3. `replication.dot` from T9.09, or a note in the report that S1 was cut.
4. `make diagrams`; embed in the report and README.
5. Check every box in the architecture diagram is a real package or goroutine in the repo.

## Acceptance

- [ ] Four PNGs (three if S1 was cut) in `docs/diagrams/out/`, each reviewed against the code by the secondary of the module it depicts.
