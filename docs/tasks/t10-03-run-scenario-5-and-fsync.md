# T10.03 Run Scenario 5: client sweep and fsync comparison

**Item:** M10 | **Milestone:** MS10 | **Week:** 12
**Suggested owner:** Ritk | **Reviewer:** Rajat
**Effort:** 3 hours of machine time
**Depends on:** T10.01, T8.01
**Unblocks:** T10.06

## Goal

The saturation point of the single-threaded loop, and the measured cost of `always` versus `everysec`.

## Steps

1. `bench/run.sh --target rfs --scenario 5 --all` and the same for `redis`. This is 4 client counts times 2 policies times 4 runs times 2 targets.
2. Plot the sweep (T8.06 chart 3) immediately and look at it: throughput should rise then flatten; the client count where it flattens is the saturation point. Note it for both targets.
3. Compute the `always` to `everysec` throughput ratio per client count for both targets. This is the durability cost figure for the report.
4. Commit results.

## Acceptance

- [ ] Sweep chart shows both targets at both policies.
- [ ] Saturation client count and durability ratio written in `notes.md`.
