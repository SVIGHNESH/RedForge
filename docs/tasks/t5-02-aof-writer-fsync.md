# T5.02 Log writer with fsync policies, wired to Propagate

**Item:** M7 | **Milestone:** MS5 | **Week:** 4
**Suggested owner:** Rajat | **Reviewer:** Ritk
**Effort:** 3 hours
**Depends on:** T5.01, T0.08
**Unblocks:** T5.03, T7.01

## Goal

Every propagated write lands in `<dir>/appendonly.aof` under the configured fsync policy, and the `everysec` goroutine is the only thing outside the loop that touches the file.

## Context you need

| Policy | Behaviour |
|---|---|
| `always` | `Write` then `Fsync` on the loop thread before the reply is queued |
| `everysec` | `Write` on the loop; a goroutine calls `Fsync` once per second when the tick asks it to |
| `no` | `Write` only |

`Fsync` on an fd concurrently with `Write` on the same fd is safe; that is why no lock is needed.

## Steps

1. `type Writer struct { f *os.File; policy Policy; buf []byte; syncReq chan struct{} }` with `Open(dir, policy)` that creates the file with the header when absent and otherwise opens for append.
2. `Append(seq, args)` encodes into `buf`, writes, and for `always` calls `f.Sync()` and returns its error. Implement the `Appender` interface from T0.08.
3. `everysec`: goroutine `for range syncReq { f.Sync() }`. The loop tick (T3.02 added it) does a non-blocking send on `syncReq` once per second. Record `aof_last_fsync_ms` and any error for INFO.
4. Wire in `main.go`: parse `--dir` and `--appendfsync`, open the writer, register it in `Srv.Appenders`.
5. On an `Append` write error, log and set `aof_last_write_status:err` in INFO; the server keeps running, matching the proposal's durability window description. Do not crash.
6. Tests: write 100 records with each policy, close, read back with `Reader` and count 100; `everysec` test asserts `Sync` was called (wrap the file in an interface for the test).

## Acceptance

- [ ] After `redis-cli SET a 1`, `xxd data/appendonly.aof` shows the header and one record whose payload is `*3\r\n$3\r\nSET\r\n$1\r\na\r\n$1\r\n1\r\n`.
- [ ] `EXPIRE` produces a `PEXPIREAT` payload.
