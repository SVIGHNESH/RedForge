# T0.08 Sequence counter and the Propagate hook

**Item:** M7, M8 (foundation) | **Milestone:** MS0 | **Week:** 2
**Suggested owner:** Rajat, since persistence depends on it | **Reviewer:** Vighnesh
**Effort:** 2 hours
**Depends on:** T0.06, T0.07
**Unblocks:** T5.02, T9.07

## Goal

Every write command's canonical form is captured with a monotonically increasing sequence number, through one hook, before any persistence code exists.
This makes the AOF, snapshot boundary, and replication stream share a single source of truth.

## Context you need

Rules from MASTER-PLAN Sections 4.4 and 4.5:

- `seq` is `uint64`, starts at 0, increments by exactly one per propagated write.
- Assigned in the dispatcher after the handler returns a non-error reply, never inside a handler.
- Read commands never call `Propagate`. A handler that fails or is a no-op does not propagate.
- `EXPIRE k 10` propagates `PEXPIREAT k <abs ms>`. `INCR k` propagates `INCR k`. `SET k v` propagates itself.

## Steps

1. `type Appender interface { Append(seq uint64, args [][]byte) error }` in `internal/command`. Add `Srv.Appenders []Appender`.
2. `Ctx` gains a private `pending [][]byte`. `Propagate(args ...[]byte)` sets it. Calling it twice in one command panics, so the mistake is caught in tests.
3. In `Dispatch`, after the handler returns: if the reply is not an `Error` and `pending != nil`, do `seq := Srv.NextSeq()` then call each appender. Also verify `cmd.Flags & Write != 0` and log a warning if a read-flagged command propagated.
4. `Srv.NextSeq()` increments and returns `Srv.seq`. `Srv.CurrentSeq()` exposes it for INFO and the snapshot. `Srv.SetSeq(n)` is used by recovery.
5. A `memoryAppender` in tests collects records so command tests can assert the propagated form.
6. Tests: a handler that returns an error does not consume a seq; two writes get seq 1 and 2; a read command with a stray `Propagate` is caught.

## Acceptance

- [ ] Sequence numbers are dense: after N successful writes `CurrentSeq() == N`.
- [ ] Error replies never advance the counter.
- [ ] `memoryAppender` available for later tasks.
