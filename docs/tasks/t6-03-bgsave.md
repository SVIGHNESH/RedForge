# T6.03 BGSAVE handler, writer goroutine, atomic rename, orphan cleanup

**Item:** M8 | **Milestone:** MS6 | **Week:** 6
**Suggested owner:** Rajat | **Reviewer:** Ritk
**Effort:** 3 to 4 hours
**Depends on:** T6.01, T6.02, T0.04
**Unblocks:** T6.04, T7.02

## Goal

`BGSAVE` produces `<dir>/dump.snap` atomically without blocking the loop for the duration of the write, and its completion is observed by the loop through epoll.

## Context you need

Sequence from MASTER-PLAN Section 4.6:

1. Reject if a save is running: `-ERR Background save already in progress`.
2. On the loop: `boundary = srv.CurrentSeq()`, `view = store.Snapshot()`, set `bgsave_in_progress`, reply `+Background saving started`.
3. Goroutine: write to `dump.snap.tmp-<pid>`, `Sync`, `Rename` to `dump.snap`, open and `Sync` the directory, write 1 to an `eventfd`.
4. Loop on `eventfd` readable: `view.Release()`, update `last_bgsave_status` (`ok` or `err`), `last_bgsave_seq = boundary`, `last_bgsave_time`.

Startup deletes `dump.snap.tmp-*` orphans (T6.04 calls this).

## Steps

1. Create the `eventfd` with `syscall.Syscall(SYS_EVENTFD2, 0, EFD_NONBLOCK|EFD_CLOEXEC, 0)` and register it for `EPOLLIN` in the server with a callback hook, so the server package does not import `snapshot`. Generalise: `Server.RegisterFd(fd, onReadable func())`.
2. `internal/snapshot/bgsave.go`: `Start(dir, boundary, view, delayMs, done func(err))`. `delayMs` is the `--debug-bgsave-delay-ms` flag and sleeps before the rename, for T7.02.
3. Register `BGSAVE` (1, Admin).
4. `CleanOrphans(dir)` removing `dump.snap.tmp-*`.
5. Tests: `BGSAVE`, poll INFO until `bgsave_in_progress:0`, read the file with `snapshot.Read` and assert `max_seq` equals `aof_current_seq` at the time of the call; second `BGSAVE` during a delayed one returns the error; kill the process mid-save via `testutil` and assert an orphan exists and `dump.snap` is either absent or the previous valid one.

## Acceptance

- [ ] `redis-cli BGSAVE` prints `Background saving started` and `dump.snap` appears.
- [ ] Loop stays responsive during a 2 s delayed save (`PING` under 5 ms).
