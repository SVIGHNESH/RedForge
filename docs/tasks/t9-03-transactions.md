# T9.03 MULTI, EXEC, DISCARD (Stretch S4)

**Item:** S4 | **Milestone:** MS9 | **Week:** 8
**Suggested owner:** Vighnesh | **Reviewer:** Rajat
**Effort:** 3 hours
**Depends on:** T0.06, T0.08; all of Vighnesh's Must items merged
**Unblocks:** S4 done

## Goal

Per-client command queueing with atomic execution on `EXEC`, Redis error semantics, and one sequence number per queued write.

## Context you need

- `MULTI` puts the client in transaction state; subsequent commands reply `+QUEUED` and are stored. Unknown command or wrong arity while queueing replies with the error and flags the transaction; `EXEC` then returns `-EXECABORT Transaction discarded because of previous errors.` and clears state.
- `EXEC` outside `MULTI` returns `ERR EXEC without MULTI`. Same for `DISCARD`.
- Commands flagged `NoTx` (`MULTI`, `EXEC`, `DISCARD`, `SUBSCRIBE`, `UNSUBSCRIBE`, `BGSAVE`) are rejected inside a transaction with `ERR Command not allowed inside a transaction`.
- Runtime errors inside `EXEC` (for example `WRONGTYPE`) do not abort; the error is placed in the reply array and later commands still run, as Redis does.
- `WATCH` is out of scope.
- Because the loop is single-threaded, `EXEC` running the queue back to back is atomic by construction. That is a viva point; write it in `DECISIONS.md`.

## Steps

1. `Client` gains `inTx bool`, `txErr bool`, `queue [][][]byte`.
2. In `Dispatch`, before the handler: if `inTx` and the command is not `EXEC`/`DISCARD`, validate name and arity, queue or set `txErr`, reply `QUEUED`.
3. `EXEC`: run each queued command through the normal path (each write propagates and gets its own seq), collect replies into an array.
4. Tests: happy path with three writes, `EXECABORT` on a bad queued command, runtime error inside `EXEC`, `NoTx` rejection, and a log-replay test showing the queued writes appear as ordinary records.

## Acceptance

- [ ] `redis-cli` session: `MULTI`, `SET a 1`, `INCR a`, `EXEC` prints `QUEUED`, `QUEUED`, then `1) OK 2) (integer) 2`.
