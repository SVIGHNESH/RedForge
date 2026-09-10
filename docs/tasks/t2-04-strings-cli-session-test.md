# T2.04 redis-cli session test for strings and keyspace

**Item:** M3 | **Milestone:** MS2 | **Week:** 4
**Suggested owner:** Vighnesh | **Reviewer:** Rajat
**Effort:** 1 hour
**Depends on:** T2.01 to T2.03, T1.02
**Unblocks:** M3 done

## Goal

The M3 definition of done is "passes the integration suite with `redis-cli`".
Lock it in as a test that drives the real client.

## Steps

1. `test/integration/cli_strings_test.go`: skip without `redis-cli`; start a server; run `redis-cli -p PORT --no-raw <cmd>` for a scripted session and compare stdout exactly.
2. Session: `SET name Vighnesh`, `GET name`, `EXISTS name`, `EXISTS nope`, `INCR counter`, `INCR counter`, `KEYS *` (sorted in the test before compare), `DEL name counter`, `GET name`.
3. Add a helper `CLI(t, port, args...) string` in `internal/testutil` for reuse by later data-type tasks.

## Acceptance

- [ ] Test passes in CI with `redis-cli` from `redis-tools`.
