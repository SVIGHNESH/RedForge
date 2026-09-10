# T2.03 KEYS

**Item:** M3 | **Milestone:** MS2 | **Week:** 4
**Suggested owner:** Vighnesh | **Reviewer:** Rajat
**Effort:** 1 to 2 hours
**Depends on:** T0.07
**Unblocks:** T8.05, expiry tests

## Goal

`KEYS pattern` returns every live key matching the glob, and its blocking cost becomes measurable through two INFO counters.

## Context you need

- The glob matcher already exists in `internal/store/glob.go` from T0.07.
- `KEYS` must not return expired keys. Walk the map, skip entries whose `ExpireAt` is due, and delete them as a side effect (this is passive expiry applied during enumeration).
- The evaluation needs to separate iteration time from reply serialisation time. Add `keys_last_iter_us` and `keys_last_reply_bytes` to the INFO `Stats` section.

## Steps

1. Register `KEYS` (arity 2, ReadOnly).
2. Iterate with `store.ForEach`, collect matching keys, record iteration microseconds, build `ArrayOfBulks`, record reply bytes.
3. Sort the result? No. Redis does not sort. Tests must compare as sets.
4. Tests: `*`, `user:*`, `?ser:1`, empty result, expired key excluded.

## Acceptance

- [ ] `redis-cli KEYS 'user:*'` returns the right keys.
- [ ] `INFO stats` shows the two counters after a `KEYS` call.
