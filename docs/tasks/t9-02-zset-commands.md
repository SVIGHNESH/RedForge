# T9.02 ZADD, ZSCORE, ZRANK, ZRANGE and persistence support (Stretch S2)

**Item:** S2 | **Milestone:** MS9 | **Week:** 8
**Suggested owner:** Ritk | **Reviewer:** Vighnesh
**Effort:** 3 hours
**Depends on:** T9.01, T6.01
**Unblocks:** S2 done

## Goal

The four sorted-set commands, the ZSET encodings in the log payload (already generic) and the snapshot, and the rank-ordering invariant test.

## Steps

1. Register `ZADD` (-4, Write; pairs of score member, no option flags), `ZSCORE` (3), `ZRANK` (3), `ZRANGE` (-4, with optional `WITHSCORES`).
2. Score parsing with `strconv.ParseFloat`; reject NaN with `ERR value is not a valid float`. Format scores in replies the way Redis does: shortest representation, integers without a decimal point (`%.17g` then trim).
3. Snapshot: implement the ZSET branch in `snapshot.Write` and `ReadInto`.
4. Invariant test: random `ZADD` with score updates against the reference, asserting `ZRANGE 0 -1 WITHSCORES` ordering and `ZRANK` for every member after each block.
5. Session test comparing `ZRANGE` output with official Redis for the same input (paste in the PR).

## Acceptance

- [ ] Session output matches Redis byte for byte for integer and fractional scores.
- [ ] CR2-style test with a sorted set survives kill and restart.
