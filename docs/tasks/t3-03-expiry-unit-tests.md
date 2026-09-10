# T3.03 Expiry invariant tests

**Item:** M9 | **Milestone:** MS3 | **Week:** 6
**Suggested owner:** Vighnesh | **Reviewer:** Rajat
**Effort:** 2 hours
**Depends on:** T3.01, T3.02
**Unblocks:** M4 done

## Goal

The `EXPIRE` and `PERSIST` invariants from Proposal Section 7 are asserted under random interleaving against a reference model.

## Steps

1. Reference model: `map[string]int64` of expiry times, with the same rules as the store.
2. Random program generator: for 1000 steps pick one of `SET`, `EXPIRE n`, `PERSIST`, `DEL`, `advance clock`, `TTL`; apply to both the store (through the command dispatcher with the `FakeClock`) and the model; after each step compare `EXISTS` and `TTL` for every key in a fixed set of 20 key names.
3. Seed from an env var so a failure is reproducible; print the seed on failure.
4. Explicit cases: `PERSIST` on no-TTL returns 0; `EXPIRE` with 0 deletes immediately and `TTL` returns -2; `SET` after `EXPIRE` clears the TTL; `EXPIRE` twice takes the later value.

## Acceptance

- [ ] Property test passes for 10 seeds in CI.
- [ ] Coverage of `internal/store/expiry.go` above 90 percent.
