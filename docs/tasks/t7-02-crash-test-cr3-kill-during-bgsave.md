# T7.02 Crash-recovery test CR3: SIGKILL during BGSAVE

**Item:** M9 | **Milestone:** MS7 | **Week:** 5 to 6
**Suggested owner:** Vivek | **Reviewer:** Rajat
**Effort:** 2 hours
**Depends on:** T7.01, `--debug-bgsave-delay-ms` flag (T6.03)
**Unblocks:** success criterion 3

## Goal

A crash mid-snapshot leaves the previous snapshot in place, leaves an orphan that startup removes, and loses no data.

## Steps

1. Start with `--debug-bgsave-delay-ms 3000 --appendfsync always`.
2. `writeMixed` prefix `a`; `BGSAVE`; wait for completion (this first save finishes after 3 s and produces a valid `dump.snap`). Record its size and checksum.
3. `writeMixed` prefix `b`; `BGSAVE` again; sleep 500 ms; `Kill()`.
4. Assert `dump.snap` is byte-identical to the recorded first snapshot, and a `dump.snap.tmp-*` file exists.
5. `Restart()`; assert the orphan is gone; `verifyMixed` for both prefixes.
6. Variant: kill during the very first `BGSAVE` (no previous snapshot); assert no `dump.snap` exists after the kill and recovery comes from the log.

## Acceptance

- [ ] Both variants pass in CI.
- [ ] Test runtime under 10 s.
