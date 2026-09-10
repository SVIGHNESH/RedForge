# T3.04 Shared-key INCR lost-update test (CC3)

**Item:** M9 | **Milestone:** MS3 | **Week:** 6
**Suggested owner:** Vighnesh | **Reviewer:** Rajat
**Effort:** 1 hour
**Depends on:** T2.02, T1.03
**Unblocks:** success criterion 4

## Goal

The test that would expose a lost update if command atomicity were ever broken.
Synopsis success criterion 4: 100 clients each issuing `INCR` N times on one key yield exactly 100 times N.

## Steps

1. `test/integration/concurrency_test.go`: start server; N = 1000; launch 100 goroutines each with its own `testutil.Client`, each doing N sequential `INCR shared`; wait; `GET shared` equals `100000`.
2. Repeat the same with each client pipelining its N `INCR`s in batches of 100, asserting the same final value.
3. Keep it under 10 s; use `-short` to drop N to 100.

## Acceptance

- [ ] Passes in CI with `-race` on the test process.
- [ ] Documented in `docs/VIVA-LEDGER.md` under "why single-threaded" as the executable evidence.
