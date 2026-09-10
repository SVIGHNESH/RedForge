# T7.01 Crash-recovery tests CR1 (log only) and CR2 (snapshot plus tail)

**Item:** M9 | **Milestone:** MS7 | **Week:** 5
**Suggested owner:** Vivek | **Reviewer:** Rajat
**Effort:** 2 to 3 hours
**Depends on:** T1.02, T1.03
**Unblocks:** M7, M8 done

## Goal

The first two of the four SIGKILL recovery tests from Proposal Section 7.
Write these from the guarantees in Proposal Section 6.4, not from Rajat's code.
If the test fails when persistence lands, the persistence code is wrong until proven otherwise.

## Steps

1. Shared fixture `writeMixed(t, c *testutil.Client, prefix string, n int)` that writes n strings, n lists of 3, n hashes of 3 fields, n sets of 3 members, and gives every fifth key `EXPIRE 1000`. Returns a model map for verification. Also `verifyMixed(t, c, model)` checking every key's type, contents, and that `TTL` is between 900 and 1000 for the expiring ones.
2. CR1: start with `--appendfsync always`; `writeMixed` 200; `Kill()`; `Restart()`; `verifyMixed`.
3. CR1 variant with `everysec`: same, but sleep 1.5 s before `Kill()` so the fsync goroutine has run. Assert everything present. Document in the test that without the sleep loss of up to one second is permitted.
4. CR2: start; `writeMixed` prefix `a`; `BGSAVE`; `WaitFor` INFO `bgsave_in_progress:0`; `writeMixed` prefix `b`; sleep 1.5 s; `Kill()`; `Restart()`; verify both; assert INFO `last_bgsave_seq` is below `aof_current_seq`.
5. Mark slow variants with `testing.Short()`.

## Acceptance

- [ ] Both tests fail against a server with persistence stubbed out (verify once on a branch) and pass once T5.04 and T6.04 merge.
