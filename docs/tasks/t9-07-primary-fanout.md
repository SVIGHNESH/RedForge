# T9.07 Primary side: replica links and write fan-out (Stretch S1)

**Item:** S1 | **Milestone:** MS9 | **Week:** 9 to 10
**Suggested owner:** Rajat | **Reviewer:** Ritk
**Effort:** 4 hours
**Depends on:** T9.06 (protocol agreed), T0.08, T6.03
**Unblocks:** T9.09

## Goal

A primary accepts `REPLSYNC`, sends a snapshot, and thereafter fans every propagated write into that replica's write buffer.

## Steps

1. Register `REPLSYNC` (2, Admin, internal). On receipt, flag the client as `replica`, take a snapshot view exactly as `BGSAVE` does but write to an in-memory buffer (or a temp file streamed afterwards) with boundary N, and queue it as a bulk string into the client's write buffer.
2. Writes that happen while the snapshot is being produced must be buffered per replica from seq N+1 onward and flushed after the bulk string. Simplest: the replica client is marked `syncing` and the fan-out appender queues records into a per-replica pending list until the snapshot bytes are queued, then drains.
3. Implement a `replicationAppender` (the `Appender` interface from T0.08) that encodes each record with `aof.Encode` and appends to every non-syncing replica's write buffer. Register it after the AOF appender.
4. No backlog is kept. A replica that disconnects and reconnects gets a full sync. Write this in `DECISIONS.md` and in the report.
5. INFO: `connected_replicas`, per replica `slave0:ip=...,seq=...` line.
6. Output-buffer limit: a replica whose write buffer exceeds 256 MB is dropped with a log line.

## Acceptance

- [ ] Two `testutil.Client`s pretending to be replicas both receive the snapshot then identical record streams for the same writes.
