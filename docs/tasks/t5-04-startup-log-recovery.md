# T5.04 Startup: open log, replay, resume sequence

**Item:** M7 | **Milestone:** MS5 | **Week:** 4
**Suggested owner:** Rajat | **Reviewer:** Ritk
**Effort:** 1 to 2 hours
**Depends on:** T5.03
**Unblocks:** T7.01 (CR1)

## Goal

The server recovers its keyspace from the log alone on start, before the snapshot exists.
T6.04 extends this with the snapshot step.

## Steps

1. `internal/recovery`: `Run(dir, store, srv) error` that, for now, replays `appendonly.aof` from seq 0 and calls `srv.SetSeq(lastSeq)`.
2. Call it from `main.go` before `Listen`. Log the number of records replayed and the duration.
3. Drop already-expired keys after replay: `store.ForEach` and delete entries whose `ExpireAt` is due; count into `expired_keys`.
4. Manual check: `SET a 1`, `EXPIRE a 100`, stop the server, restart, `GET a` and `TTL a`.

## Acceptance

- [ ] Restart with a clean stop recovers all keys and TTLs are aged.
- [ ] Log line reports records replayed.
