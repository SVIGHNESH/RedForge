# T7.03 Crash-recovery test CR4: hand-truncated log record

**Item:** M9 | **Milestone:** MS7 | **Week:** 6
**Suggested owner:** Vivek | **Reviewer:** Rajat
**Effort:** 1 to 2 hours
**Depends on:** T7.01
**Unblocks:** success criterion 3

## Goal

A partially written record at the end of the log is discarded, the file is truncated to the last valid boundary, the server starts, and INFO reports it.

## Steps

1. Start with `--appendfsync always`; write 50 keys `k0..k49` with `SET`; `Kill()`.
2. Open `<dir>/appendonly.aof`, get its size, truncate it by 7 bytes (this cuts into the last record's payload).
3. `Restart()`; assert `GET k49` is nil and `GET k48` is present; assert INFO `aof_truncated_records:1` and `aof_last_truncation_offset` equals the file size after your truncation minus the length of the partial record (compute from the record header, or just assert it is less than the truncated size and greater than zero).
4. Assert the file size after restart is smaller than after your truncation (the server truncated further, to the boundary).
5. Write `SET k49 again`; `Kill()`; `Restart()`; assert present. This proves the server appends correctly after truncation.
6. Variant: corrupt one byte in the middle of the file instead of truncating; assert every key before that record is present and every key after is absent.

## Acceptance

- [ ] Both variants pass.
