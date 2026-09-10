# T9.06 REPLICAOF, replica state machine and handshake (Stretch S1)

**Item:** S1 | **Milestone:** MS9 | **Week:** 9 to 10
**Suggested owner:** Vighnesh | **Reviewer:** Ritk
**Effort:** 6 hours
**Depends on:** Week 8 checkpoint passed; T6.01, T5.01
**Unblocks:** T9.07, T9.08

## Goal

A server can be told to follow a primary, connects to it, receives a full snapshot, and then a stream of log records.

## Context you need

The handshake is our own, not Redis's `PSYNC`, because client compatibility is the requirement and replica compatibility is not.
Wire protocol, all over one TCP connection the replica opens:

```
replica -> primary   *2 REPLSYNC <last_seq>            (RESP array; last_seq ignored in this version, always full sync)
primary -> replica   $<len>\r\n<snapshot bytes>\r\n    (bulk string containing a snapshot file image with boundary N)
primary -> replica   stream of log records in the T5.01 record format, seq > N, forever
```

States: `Disconnected`, `Connecting`, `Syncing` (reading the bulk snapshot), `Streaming`.
On any error return to `Disconnected` and retry every 1 s from the tick.

## Steps

1. `internal/replication/replica.go`: `REPLICAOF host port` (3, Admin) sets `role:slave` fields and starts the state machine; `REPLICAOF NO ONE` disconnects and returns to `role:master`.
2. The primary connection is a non-blocking socket registered in epoll with its own readable callback (use `Server.RegisterFd` from T6.03), so no goroutine.
3. `Syncing`: accumulate the bulk string, load it with `snapshot.ReadInto` into a fresh store, swap stores, set `srv.SetSeq(N)`.
4. `Streaming`: feed bytes to `aof.Reader` incrementally (add an incremental `Feed` mode to the reader if needed) and hand records to the apply path (T9.08).
5. INFO `Replication` section: `role`, `master_host`, `master_port`, `master_link_status`, `replication_seq`.
6. Tests at this stage: unit tests of the state machine with a fake primary socket in the test process.

## Acceptance

- [ ] State machine unit tests cover connect failure, mid-snapshot disconnect, and stream disconnect with reconnect.
- [ ] `REPLICAOF NO ONE` on a server that was never a replica returns `OK` and changes nothing.
