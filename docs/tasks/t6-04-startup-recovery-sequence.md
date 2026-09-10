# T6.04 Full startup recovery: snapshot plus log tail

**Item:** M8 | **Milestone:** MS6 | **Week:** 6
**Suggested owner:** Rajat | **Reviewer:** Ritk
**Effort:** 2 hours
**Depends on:** T5.04, T6.01, T6.03
**Unblocks:** T7.01 (CR2), T7.03, T7.04

## Goal

The startup sequence of MASTER-PLAN Section 4.9 is implemented exactly, and a corrupt snapshot degrades to log-only recovery instead of a crash.

```
delete dump.snap.tmp-*
if dump.snap exists and verifies: load it, seq = max_seq; else log a warning
open appendonly.aof (create with header if missing)
replay records with seq > max_seq; stop and truncate at the first bad record
drop every key whose expire_at <= now; count them as expired_keys
seq = max(seq, last replayed seq)
start listening
```

## Steps

1. Extend `recovery.Run` with the snapshot step before replay, passing `max_seq` as `afterSeq` to `Replay`.
2. If `snapshot.Read` fails, log the reason, start from an empty store, and replay from seq 0. Expose `snapshot_last_load_status` in INFO.
3. Log one line per step with counts and durations.
4. Tests: write, `BGSAVE`, write more, clean stop, restart, all present and `aof_current_seq` correct; corrupt `dump.snap` by flipping a byte, restart, server starts and recovers from the log alone.

## Acceptance

- [ ] Both tests pass.
- [ ] Startup on an empty dir creates the log with its header and no snapshot.
