# T4.03 List mixed-end property test

**Item:** M9 | **Milestone:** MS4 | **Week:** 4
**Suggested owner:** Ritk | **Reviewer:** Vighnesh
**Effort:** 1 to 2 hours
**Depends on:** T4.02
**Unblocks:** M5 partial

## Goal

Proposal Section 7 invariant: any interleaving of pushes and pops at both ends leaves length and order as a reference model predicts.

## Steps

1. Reference model: a Go slice with `append` at front and back.
2. Generate 5000 random operations from {`LPUSH`, `RPUSH`, `LPOP`, `RPOP`} with random 1 to 3 values, seeded from an env var; apply to the server through `testutil.Client` and to the model.
3. After every 50 operations compare `LRANGE l 0 -1` and `LLEN l` with the model. Also assert `EXISTS l` is 0 exactly when the model is empty.
4. Run with three seeds in CI.

## Acceptance

- [ ] Passes with three seeds; seed printed on failure.
