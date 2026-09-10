# T4.06 Hash and set invariant tests

**Item:** M9 | **Milestone:** MS4 | **Week:** 6
**Suggested owner:** Ritk | **Reviewer:** Vighnesh
**Effort:** 1 to 2 hours
**Depends on:** T4.04, T4.05
**Unblocks:** M5, M6 done

## Goal

Random interleavings of `HSET`/`HDEL` and `SADD`/`SREM` match a reference model, including key disappearance at zero size.

## Steps

1. Reuse the property-test skeleton from T4.03.
2. Hash model: `map[string]string`; after each block compare `HGETALL` as a map, `HLEN`, and `EXISTS` equals `len(model) > 0`.
3. Set model: `map[string]bool`; compare sorted `SMEMBERS`, and assert that `SADD` of an existing member returns 0 and leaves cardinality unchanged.
4. `SINTER` check: maintain two model sets, compare sorted intersection.

## Acceptance

- [ ] Both property tests pass with three seeds in CI.
