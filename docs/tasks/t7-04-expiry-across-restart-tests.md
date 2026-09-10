# T7.04 Expiry tests EX1 to EX3 (across restart and passive)

**Item:** M9 | **Milestone:** MS7 | **Week:** 6
**Suggested owner:** Vivek | **Reviewer:** Rajat
**Effort:** 1 to 2 hours
**Depends on:** T7.01, T3.01
**Unblocks:** success criterion 2

## Goal

Proposal Section 6.3 as executable tests: a key that expired while the server was down is absent, and one that did not has a TTL reduced by the offline interval.

## Steps

1. EX1: `SET s v`, `EXPIRE s 2`, `BGSAVE`, wait complete, `Kill()`, sleep 3 s, `Restart()`; assert `GET s` nil, `EXISTS s` 0, `KEYS *` does not include `s`, INFO `expired_keys` at least 1.
2. EX2: `SET s v`, `EXPIRE s 100`, `BGSAVE`, wait, `Kill()`, sleep 5 s, `Restart()`; assert `TTL s` between 93 and 96.
3. EX2 log-only variant: same without `BGSAVE`, proving the `PEXPIREAT` rewrite in the log carries the absolute time.
4. EX3: `SET s v`, `EXPIRE s 1`, sleep 2 s, `GET s` nil, `expired_keys` incremented by exactly 1.
5. EX4 lives in T3.02 already; reference it here so the four expiry tests are enumerated in one file's doc comment.

## Acceptance

- [ ] All pass in CI. Wall-clock sleeps are the point; do not use the fake clock here.
