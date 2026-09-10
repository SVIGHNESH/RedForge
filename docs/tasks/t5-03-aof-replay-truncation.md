# T5.03 Replay, torn-record truncation, INFO fields

**Item:** M7 | **Milestone:** MS5 | **Week:** 4
**Suggested owner:** Rajat | **Reviewer:** Ritk
**Effort:** 3 hours
**Depends on:** T5.01, T5.02, T0.06
**Unblocks:** T5.04, T7.03

## Goal

The log can be replayed through the dispatcher without propagating or replying, stopping and truncating at the first bad record.

## Context you need

- Replay must not assign new sequence numbers, must not append to the log, and must not send replies. Add a `Replaying bool` to `Ctx` (or a dispatcher option) that disables `Propagate` side effects and reply delivery.
- Time during replay: `PEXPIREAT` records carry absolute times, so `ctx.Now` is the real current time and keys that are already due simply get an expiry in the past. The recovery step in T6.04 drops them.
- Torn policy from Proposal Section 6.4: discard the bad record and everything after, truncate the file to the last valid boundary, log it, surface in INFO.

## Steps

1. `Replay(path string, afterSeq uint64, apply func(seq uint64, args [][]byte)) (lastSeq uint64, result ReplayResult, err error)`. Read records; skip those with `seq <= afterSeq`; call `apply` for the rest. On `ErrTorn` or `ErrCorrupt`, record `TruncatedAt = LastGoodOffset`, truncate the file with `os.Truncate`, and return normally.
2. `apply` in `main.go` calls `command.Dispatch` in replay mode.
3. INFO fields `aof_last_truncation_offset` (-1 when none) and `aof_truncated_records` (0 or 1).
4. Tests: replay a clean file and assert store contents; replay a file with `afterSeq` set to skip the first two; replay a hand-truncated file and assert the file is now shorter and the fields are set; replay a file with a corrupt CRC in the middle and assert records after it are gone from the file.

## Acceptance

- [ ] All replay tests pass.
- [ ] Replaying twice gives the same store (idempotence).
