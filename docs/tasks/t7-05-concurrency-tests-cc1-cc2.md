# T7.05 Concurrency tests CC1 (disjoint writes) and CC2 (pipelined ordering)

**Item:** M9 | **Milestone:** MS7 | **Week:** 6
**Suggested owner:** Vivek | **Reviewer:** Rajat
**Effort:** 1 to 2 hours
**Depends on:** T1.03
**Unblocks:** M9 done

## Goal

The remaining two concurrency invariants from Proposal Section 7. CC3 is T3.04.

## Steps

1. CC1: 100 goroutine clients, each writes `SET c<i>:k<j> <i>-<j>` for j in 0..99; wait; one client verifies all 10000 keys with `GET`, then `KEYS c*` has 10000 entries, then INFO `db0:keys=10000`.
2. CC2: 10 clients, each pipelines 1000 `INCR own<i>` in one `Pipeline` call; assert the 1000 replies are exactly 1, 2, ..., 1000 in order; final `GET own<i>` is 1000.
3. CC2 variant: each client pipelines a mix of `SET x<i> v`, `GET x<i>`, `INCR y<i>` and asserts the replies come back in issue order with the right types.

## Acceptance

- [ ] Pass under `-race` in CI in under 10 s.
