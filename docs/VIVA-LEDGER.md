# Viva Ledger

Started in Week 3 from the IMPLEMENTATION-PLAN "Viva Preparation Ledger" section, and updated after every weekly sync (T11.05).
Each question has a three-sentence answer, the evidence that backs it, and two members who can say it aloud.
A name goes in only after that person has answered the question cold at a sync or a rehearsal.
Answers come from PROPOSAL.md, MASTER-PLAN.md and DECISIONS.md; when one of those changes, the answer here changes with it.

## Status

| # | Question | Members who can answer | Last checked |
|---|---|---|---|
| 1 | Why single-threaded, and what does it cost? | _, _ | - |
| 2 | Why absolute expiry timestamps? | _, _ | - |
| 3 | Crash during `BGSAVE`? | _, _ | - |
| 4 | Partially written AOF record? | _, _ | - |
| 5 | Snapshot and AOF disagree? | _, _ | - |
| 6 | Why keep `KEYS`? | _, _ | - |
| 7 | What replication gives and does not give | _, _ | - |
| 8 | Where slower than Redis, and why | _, _ | - |

## 1. Why single-threaded, and what does it cost in measured terms?

**Answer.**
One event loop over raw `epoll` owns the whole keyspace, so every command runs atomically without a single lock, which removes lock ordering, deadlocks and contention by construction.
The price is that throughput is bounded by one core, and any O(N) command blocks every other client while it runs.
The measured cost is the Scenario 5 client sweep against official Redis, which locates the point where the loop saturates.

**Evidence.**
PROPOSAL 6.1; DECISIONS D1 and D2; `TestHundredConnectionsAddNoGoroutines` (internal/server and test/integration); Scenario 5 results (T10.03, pending).
The executable proof of atomicity is CC3 (T3.04): `TestSharedKeyIncrLosesNoUpdates` and `TestSharedKeyPipelinedIncrLosesNoUpdates` in `test/integration/concurrency_test.go`.
100 clients each send 1000 `INCR` on one key, one at a time and then pipelined in batches of 100, and the final value must be exactly 100000.
A lost update, which is what breaking single-threaded atomicity would cause, leaves a smaller number.

## 2. Why absolute expiration timestamps rather than remaining TTL?

**Answer.**
A remaining TTL saved in a snapshot would restart its countdown after a restart, so a key with 100 s left that sat offline for 50 s would come back with 100 s instead of 50 s.
Storing the absolute Unix-millisecond deadline makes recovery correct, and a key that expired while the server was down stays gone.
For the same reason `EXPIRE` is logged and replicated as `PEXPIREAT` with the absolute time, so replay does not depend on when it runs.

**Evidence.**
PROPOSAL 6.3; MASTER-PLAN 4.4; EX1 and EX2 (T7.04); `EXPIRE` propagation tests (T3.01).
Known limit: a backwards wall-clock jump extends lifetimes; the mitigation is a monotonic clock plus boot offset, listed as future work.

## 3. What happens on a crash during `BGSAVE`?

**Answer.**
The snapshot is written to `dump.snap.tmp-<pid>`, fsynced, renamed over `dump.snap`, and then the directory is fsynced, so the rename is the only moment the new snapshot becomes live.
A crash before the rename leaves the previous `dump.snap` untouched plus an orphaned temp file, which startup deletes.
Nothing is lost, because the log is never cut back on the strength of a snapshot that was not renamed, and replaying extra log is harmless.

**Evidence.**
PROPOSAL 6.4; MASTER-PLAN 4.6 and 4.9; DECISIONS D6; CR3 kill-during-`BGSAVE` test (T7.02).

## 4. What happens to a partially written AOF record?

**Answer.**
Every record carries its sequence number, a length and a CRC32, so replay can tell a torn record from a good one.
Replay stops at the first record that is short or fails its checksum, truncates the file to where that record began, logs it, and reports it in INFO as `aof_truncated_records` and `aof_last_truncation_offset`.
Discarding it is correct because a torn record is a write that was never acknowledged as durable.

**Evidence.**
PROPOSAL 6.4; MASTER-PLAN 4.7; CR4 torn-record test (T7.03); codec corruption tests (T5.01).

## 5. If the snapshot and the AOF disagree, which wins, and why can they not actually disagree?

**Answer.**
Every write gets a dense sequence number on the loop thread, and the snapshot records `max_seq`, the highest sequence it contains.
The snapshot owns everything up to `max_seq` and the log owns everything after it, because replay applies only records with a higher sequence.
The two files therefore describe different ranges of history rather than competing views, which also makes replay idempotent and safe to retry.

**Evidence.**
PROPOSAL 6.4; MASTER-PLAN 4.5 and 4.9; `TestPropagatedWritesGetDenseSequenceNumbers` (internal/command); CR2 snapshot-plus-tail test (T7.01).

## 6. Why is `KEYS` a supported command in a project that emphasises non-blocking service?

**Answer.**
Client tooling and the integration tests need to enumerate the keyspace, so `KEYS` stays as a Must-Have.
It is O(N) and blocks every client for the whole scan, and we say so rather than hide it.
Instead of building `SCAN`, which is out of scope, we measure the event-loop stall at 10^4, 10^5 and 10^6 keys, so the cost is a number in the report.

**Evidence.**
PROPOSAL 6.2; KEYS stall measurement (T8.05, T10.04, pending).

## 7. What does replication give you, and what does it explicitly not give you?

**Answer.**
A replica sends `SYNC <last_seq>`, receives a full snapshot, then applies the primary's log records with the primary's sequence numbers, which gives read scaling and a redundant copy of the data.
Replicas reject writes from clients with `READONLY` and run their own expiry from the absolute timestamps.
It gives no automatic failover and no high availability, and with no backlog every reconnect is a full sync.

**Evidence.**
PROPOSAL 1 and 4.4; MASTER-PLAN MS9 S1; two-replica topology test (T9.09, stretch, pending).

## 8. Where is this implementation slower than Redis, and which specific design decision causes it?

**Answer.**
This answer is filled in from the Week 12 evaluation; until then only the candidate causes are known.
The candidates are Go's garbage collector versus manual memory management, no specialised small-object allocator, no compact encodings such as listpack for small collections, and per-command allocation in the RESP codec.
Each gap in the report is tied to one of these by a targeted experiment, for example a `GOGC=off` run to size the GC share.

**Evidence.**
PROPOSAL 8; MASTER-PLAN MS10 step 7; attribution experiments (T10.05, pending).

## Questions added in reviews

T11.05 step 3 adds questions here as they come up in reviews, for example "why eventfd", "why not fork" and "why no replication backlog".
Each one gets the same three parts: answer, evidence, two names.
