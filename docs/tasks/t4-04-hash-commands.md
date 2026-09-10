# T4.04 Hash commands

**Item:** M5 | **Milestone:** MS4 | **Week:** 5
**Suggested owner:** Ritk | **Reviewer:** Vighnesh
**Effort:** 2 to 3 hours
**Depends on:** T0.07, T0.06, T0.08
**Unblocks:** T4.06, T6.01

## Goal

`HSET`, `HGET`, `HDEL`, `HLEN`, `HGETALL` with the zero-fields-key-disappears invariant.

## Context you need

- `HSET k f v [f v ...]` returns the number of new fields added (updates do not count). Odd argument count is a wrong-arguments error.
- `HDEL k f [f ...]` returns the number removed; a hash left with zero fields is deleted as a key.
- `HGETALL` returns a flat array `f1 v1 f2 v2`. Order is unspecified; tests compare as maps.
- `HGET` counts as a keyspace hit or miss.
- All mutations through `store.Mutable`. Copy field and value bytes out of the read buffer.

## Steps

1. `internal/command/hashes.go`: register `HSET` (-4), `HGET` (3), `HDEL` (-3), `HLEN` (2), `HGETALL` (2).
2. `HashVal.M map[string][]byte` with `Clone` doing a shallow map copy (values are immutable byte slices that are replaced, not edited).
3. Propagate `HSET` as received, `HDEL` with only the fields that existed and only if at least one did.
4. Session test: `HSET h a 1 b 2` expecting 2, `HSET h a 9` expecting 0, `HGET h a` expecting 9, `HLEN h`, `HGETALL h`, `HDEL h a b`, `EXISTS h` expecting 0.

## Acceptance

- [ ] Session test passes.
- [ ] `HGETALL` of a missing key prints `(empty array)` in `redis-cli`.
