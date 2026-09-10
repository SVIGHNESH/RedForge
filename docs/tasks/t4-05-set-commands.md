# T4.05 Set commands

**Item:** M6 | **Milestone:** MS4 | **Week:** 6
**Suggested owner:** Ritk | **Reviewer:** Vighnesh
**Effort:** 2 to 3 hours
**Depends on:** T0.07, T0.06, T0.08
**Unblocks:** T4.06, T6.01

## Goal

`SADD`, `SREM`, `SISMEMBER`, `SMEMBERS`, `SINTER` with cardinality invariance under duplicate adds.

## Context you need

- `SADD k m [m ...]` returns the number of members actually added.
- `SREM` returns the number removed; empty set deletes the key.
- `SINTER k1 [k2 ...]` on a missing key returns an empty array (a missing set is empty). Iterate the smallest set and probe the others to keep it O(N*M) worst case, O(smallest) typical.
- `SMEMBERS` order is unspecified; tests compare sorted.

## Steps

1. `internal/command/sets.go`: register `SADD` (-3), `SREM` (-3), `SISMEMBER` (3), `SMEMBERS` (2), `SINTER` (-2).
2. `SetVal.M map[string]struct{}` with `Clone`.
3. Propagate `SADD` and `SREM` only with the members that changed state, and only if any did.
4. Session test: `SADD s a b a` expecting 2, `SISMEMBER s a`, `SADD t b c`, `SINTER s t` expecting `b`, `SREM s a b`, `EXISTS s` expecting 0.

## Acceptance

- [ ] Session test passes.
- [ ] `SINTER` with one missing key returns empty and does not error.
