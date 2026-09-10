# Project Proposal: An In-Memory Data Store Implementing a Defined Subset of the Redis Command Set and the RESP2 Protocol

**Course:** 7th Semester Major Project
**Team size:** 4
**Language:** Go
**Repository:** `redis-from-scratch`

## 1. Abstract

An in-memory data store built from scratch in Go, implementing a defined subset of the Redis command set over the RESP2 wire protocol, such that standard Redis client tooling including `redis-cli` and `redis-benchmark` connects to it unmodified.
The server is structured as a single-threaded event loop over non-blocking TCP sockets, a design chosen to eliminate lock contention on shared in-memory state and to study event-driven server architecture directly, rather than to maximise CPU parallelism.
It supports strings, lists, hashes, and sets, key expiration using absolute expiration timestamps with both passive and active reclamation, and, as extensions, sorted sets backed by a skiplist, transactions, and Pub/Sub messaging.
Durability is provided by a hybrid persistence layer combining point-in-time binary snapshots with an append-only log, recovered at startup by loading the snapshot and replaying only the log entries that follow the snapshot's logical boundary, with defined behaviour for crashes during snapshot writes and for partially written log records.
The system further implements asynchronous primary-replica replication, propagating writes from a primary to read-only replicas for read scaling and improved data redundancy; automatic failover is out of scope, so the system does not claim high availability.
Evaluation is conducted against the official Redis server under a controlled methodology spanning five workload scenarios, reporting throughput, p50, p95, p99, and p99.9 latency, CPU utilisation, and memory footprint, with a written analysis attributing observed differences to specific architectural decisions.

## 2. Academic Framing

The central claim of this project is deliberately narrow.

> This project does not attempt to reproduce Redis.
> It uses a production-grade system as a reference point for studying the design and implementation of an in-memory server, and implements the core engineering principles behind it.

The word "compatible" is used throughout this document in a bounded sense: compatible with the RESP2 protocol and with the specific command subset enumerated in Section 5, sufficient for unmodified Redis client tooling to operate against the server within that subset.
It is not a claim of general Redis compatibility.
Section 4.4 enumerates precisely what is excluded.

## 3. Motivation

Most database systems are studied as black boxes.
This project opens the box by rebuilding the core of one of the most widely deployed data stores in production today.
The work spans network programming, protocol design, data structure implementation, durability engineering, and replication, which together cover the practical core of a systems engineering curriculum.

Because the implementation speaks the real RESP2 protocol, correctness is externally verifiable.
The official `redis-cli` and `redis-benchmark` tools act as an independent test harness that the team did not write and cannot bias.

## 4. Scope and the Cut Line

This is the most important section of this document.
The team commits to the following division before any code is written.
The Must-Have set defines project success.
The Stretch set is explicitly optional and will be cut without renegotiation if the schedule slips.

### 4.1 Must-Have (project fails without these)

These items are non-negotiable and are the sole basis on which the team considers the project complete.

| # | Item | Definition of done |
|---|------|--------------------|
| M1 | RESP2 protocol codec | `redis-cli` connects, issues commands, and renders replies correctly with no custom client code |
| M2 | Non-blocking TCP event loop | 100 concurrent clients served without a goroutine-per-connection blowup |
| M3 | Core string commands | `SET`, `GET`, `DEL`, `EXISTS`, `INCR`, `KEYS` pass the integration suite |
| M4 | Key expiry | `EXPIRE`, `TTL`, `PERSIST`, using absolute expiration timestamps, with both passive (on-access) and active (background sweep) reclamation |
| M5 | Lists and hashes | `LPUSH`, `RPUSH`, `LPOP`, `RPOP`, `LRANGE`, `LLEN`, `HSET`, `HGET`, `HGETALL`, `HDEL` |
| M6 | Sets | `SADD`, `SREM`, `SMEMBERS`, `SISMEMBER`, `SINTER` |
| M7 | AOF persistence | Server restart recovers full keyspace from the append-only log, with a defined torn-record policy |
| M8 | Snapshot persistence | `BGSAVE` writes a binary snapshot atomically; server boots from snapshot plus the log tail after the snapshot boundary |
| M9 | Correctness test suite | State-invariant, crash-recovery, expiration, and concurrent-client tests as specified in Section 7 |
| M10 | Evaluation report | Five benchmark scenarios under the controlled methodology of Section 8, compared against official Redis, with the gap explained in writing |
| M11 | Documentation | README, the four architecture diagrams of Section 9, build and run instructions, and the final report |

### 4.2 Stretch Goals (cut first, in this order)

These are attempted only after the entire Must-Have set is complete and merged.
They are listed in the order they will be abandoned, last item cut first.

| # | Item | Cut order |
|---|------|-----------|
| S1 | Primary-replica replication (`REPLICAOF`, write propagation, read-only replicas) | Cut 5th |
| S2 | Sorted sets with skiplist (`ZADD`, `ZRANGE`, `ZRANK`, `ZSCORE`) | Cut 4th |
| S3 | AOF rewrite and compaction | Cut 3rd |
| S4 | Transactions (`MULTI`, `EXEC`, `DISCARD`) | Cut 2nd |
| S5 | Pub/Sub (`SUBSCRIBE`, `UNSUBSCRIBE`, `PUBLISH`) | Cut 1st |

### 4.3 Cut Line Enforcement

The cut line is reviewed at the end of Week 8 and again at the end of Week 11.
At each checkpoint, any Must-Have item that is not merged into `main` takes priority over every Stretch item, and the team reassigns people onto it immediately.
A Stretch item that is in progress when a Must-Have item is behind gets abandoned on the branch rather than finished.

No Stretch item may be started by any member until every Must-Have item owned by that member is merged.

### 4.4 Explicitly Out of Scope

Named here so that no reviewer mistakes their absence for an oversight, and so that no team member quietly starts building one.

| Excluded | Consequence of exclusion |
|----------|--------------------------|
| Cluster mode, consistent hashing, `MOVED` and `ASK` redirects | Single-node keyspace only; no horizontal write scaling |
| Sentinel-style automatic failover | Replication provides redundancy, not automatic high availability |
| RESP3 protocol | Clients must negotiate RESP2; `HELLO 3` is rejected |
| ACLs and TLS | No authentication or transport encryption; not deployable on an untrusted network |
| Lua scripting via `EVAL` | No server-side atomic multi-command scripting beyond `MULTI`/`EXEC` |
| Streams, HyperLogLog, geospatial, bitmaps | Four data types supported, not the full Redis type system |
| `SCAN` and cursor-based iteration | `KEYS` is the only keyspace enumeration primitive; see Section 6 |

Cluster mode is the most defensible omission and is the single item the team would build next given more time.
It is discussed as future work in the final report.

## 5. Command Support Matrix

Status values are `Must` (Section 4.1), `Stretch` (Section 4.2), and `Excluded` (Section 4.4).
Complexity is stated for the implementation in this project, which may differ from Redis where the underlying representation differs.

| Category | Command | Complexity | Status |
|----------|---------|-----------|--------|
| Connection | `PING` | O(1) | Must |
| Connection | `ECHO` | O(1) | Must |
| Server | `INFO` | O(1) | Must |
| Server | `COMMAND` | O(1) | Must |
| Server | `BGSAVE` | O(N) background | Must |
| Strings | `SET` | O(1) | Must |
| Strings | `GET` | O(1) | Must |
| Strings | `DEL` | O(1) per key | Must |
| Strings | `EXISTS` | O(1) per key | Must |
| Strings | `INCR` | O(1) | Must |
| Keyspace | `KEYS` | O(N) blocking | Must |
| Expiration | `EXPIRE` | O(1) | Must |
| Expiration | `TTL` | O(1) | Must |
| Expiration | `PERSIST` | O(1) | Must |
| Lists | `LPUSH` / `RPUSH` | O(1) amortised | Must |
| Lists | `LPOP` / `RPOP` | O(1) | Must |
| Lists | `LLEN` | O(1) | Must |
| Lists | `LRANGE` | O(S+N) | Must |
| Hashes | `HSET` / `HGET` / `HDEL` | O(1) average | Must |
| Hashes | `HLEN` | O(1) | Must |
| Hashes | `HGETALL` | O(N) | Must |
| Sets | `SADD` / `SREM` / `SISMEMBER` | O(1) average | Must |
| Sets | `SMEMBERS` | O(N) | Must |
| Sets | `SINTER` | O(N*M) worst case | Must |
| Sorted sets | `ZADD` / `ZSCORE` / `ZRANK` | O(log N) | Stretch |
| Sorted sets | `ZRANGE` | O(log N + M) | Stretch |
| Transactions | `MULTI` / `EXEC` / `DISCARD` | O(N) queued commands | Stretch |
| Pub/Sub | `SUBSCRIBE` / `UNSUBSCRIBE` | O(1) | Stretch |
| Pub/Sub | `PUBLISH` | O(N) subscribers | Stretch |
| Replication | `REPLICAOF` | O(N) initial sync | Stretch |
| Keyspace | `SCAN` | - | Excluded |
| Scripting | `EVAL` | - | Excluded |
| Streams | `XADD` and family | - | Excluded |

## 6. Architectural Decisions and Their Justification

### 6.1 Why a single-threaded event loop

The request path is:

```
TCP connections
      ↓
non-blocking sockets
      ↓
event loop
      ↓
RESP command parser
      ↓
command dispatcher
      ↓
single shared store
```

The goal of this architecture is not to maximise CPU parallelism.
It is to eliminate lock contention around shared in-memory state, so that every command executes atomically by construction rather than by explicit synchronisation, and to study event-driven server architecture in a setting where the concurrency model is the object of study rather than an implementation detail.

Three consequences follow directly, and the team accepts all three.

1. Command atomicity is free. No mutex is required around the keyspace, so no lock ordering, no deadlock class, and no contention benchmark.
2. Throughput is bounded by one core. The design deliberately trades peak throughput for a simpler and more analysable execution model.
3. Any O(N) command blocks all other clients for its duration. This is the direct cause of the `KEYS` trade-off discussed in Section 6.2.

Redis makes the same choice, but Redis making it is not the justification.
The justification is that lock-free shared state and an analysable execution model are worth more to this project than multi-core throughput, and the evaluation in Section 8 quantifies exactly what that costs.

### 6.2 The `KEYS` trade-off

`KEYS` is O(N) over the entire keyspace and, under the single-threaded model above, blocks every other client for the full duration of the scan.
This stands in tension with the project's emphasis on event-driven, non-blocking service.

The command is nonetheless retained as a Must-Have, for two reasons: client tooling and the integration test suite both depend on keyspace enumeration, and its inclusion makes the blocking cost concrete and measurable rather than hypothetical.

The team acknowledges this explicitly rather than concealing it.
Redis addresses the same problem with `SCAN`, a cursor-based incremental iterator that returns a bounded number of keys per call and permits the event loop to service other clients between calls, at the cost of weaker guarantees: keys added or removed mid-iteration may be missed or returned twice.
`SCAN` is out of scope for this project.
The final report documents the limitation and includes a measurement of event-loop stall time under `KEYS` against a large keyspace, turning the omission into a quantified result.

### 6.3 Expiration uses absolute timestamps

Each key with a TTL stores an **absolute expiration timestamp** in Unix milliseconds, not a remaining duration.

This is required for correctness across restarts.
Consider:

```
SET A hello
EXPIRE A 100
BGSAVE
<server offline for 50 seconds>
<restart>
TTL A
```

If the snapshot stored the remaining TTL of 100 seconds, the restarted server would report approximately 100, effectively resurrecting 50 seconds of lifetime that wall-clock time has already consumed.
Storing the absolute expiry timestamp yields approximately 50, which is correct.
The same argument applies with more force when the offline interval exceeds the TTL: a key that expired while the server was down must be absent after recovery, not revived.

Consequences the team accepts and documents:

- Expiration is tied to system wall-clock time, so a backwards clock adjustment can extend key lifetimes. The report notes this and identifies a monotonic-clock-plus-boot-offset scheme as the mitigation used by production systems.
- Both the snapshot format and the AOF record for `EXPIRE` persist the absolute timestamp, never the relative argument the client supplied. The AOF stores a rewritten `PEXPIREAT`-style record rather than the literal `EXPIRE` the client issued, so that log replay is time-independent.

Reclamation is two-tier: **passive**, where an expired key found on lookup is deleted and reported absent, and **active**, where a background sweep samples the expiring-key set at a fixed interval to reclaim keys that are never accessed again.
Neither tier alone is sufficient; passive alone leaks memory for untouched keys, and active alone permits a window in which an expired key is still visible to a reader.

### 6.4 Persistence and crash-recovery guarantees

Startup sequence:

```
startup
   ↓
load snapshot (if present and valid)
   ↓
read snapshot's logical sequence boundary
   ↓
replay AOF records with sequence > boundary
   ↓
reconstruct expiration metadata; drop already-expired keys
   ↓
serve clients
```

The following guarantees are defined now, before implementation, and are tested by M9.

**Which source wins.**
The snapshot and the AOF are not competing views of the same data and cannot disagree.
The snapshot is authoritative for all state up to its logical sequence boundary; the AOF is authoritative for everything after it.
Every write is assigned a monotonically increasing sequence number, the snapshot records the highest sequence it contains, and replay skips every AOF record at or below that number.
This makes replay idempotent and makes a crash mid-replay safe to retry.

**Crash during `BGSAVE`.**
The snapshot is written to a temporary file and made live by an atomic rename only after the write completes and is fsynced.
A crash mid-write therefore leaves the previous valid snapshot in place and an orphaned temporary file, which is deleted on the next startup.
The AOF is never truncated on the basis of a snapshot that has not been successfully renamed.
Worst case, recovery replays more of the log than strictly necessary, which is harmless because replay is idempotent.

**Partially written AOF record.**
Each record carries a length prefix and a checksum.
On replay, a record that is truncated or fails its checksum terminates replay at that point; the record and everything after it are discarded, and the file is truncated to the last valid record boundary.
This is the standard write-ahead-log torn-write policy: a partial record represents a write that was never acknowledged as durable, so discarding it is correct.
The event is logged and surfaced in `INFO`.

**Durability window.**
The AOF fsync policy is configurable as `always`, `everysec`, or `no`, defaulting to `everysec`.
Under the default, up to one second of acknowledged writes may be lost on power failure.
This is a deliberate durability-versus-throughput trade-off, and Scenario 5 of Section 8 measures its cost by benchmarking `always` against `everysec`.

## 7. Correctness Properties and Test Design

M9 is not satisfied by per-command smoke tests.
The suite asserts state invariants and recovery properties.

**State invariants.**
For each of the following pairs, an arbitrary interleaving of operations leaves the keyspace in the state a reference model predicts.

- `LPUSH` / `RPUSH` with `LPOP` / `RPOP`, including length and ordering after mixed-end operations.
- `HSET` with `HDEL`, including the invariant that a hash with zero remaining fields ceases to exist as a key.
- `SADD` with `SREM`, including set-cardinality invariance under duplicate `SADD`.
- `EXPIRE` with `PERSIST`, including that `PERSIST` on a key with no TTL is a no-op returning 0.
- `ZADD` with score update, asserting rank ordering is maintained (Stretch, if S2 survives).

**Crash-recovery tests.**
Each of these writes data, kills the process with `SIGKILL` rather than a clean shutdown, restarts, and verifies state.

- Write, kill, restart, verify full keyspace from AOF alone.
- Write, `BGSAVE`, write more, kill, restart, verify snapshot plus log tail.
- Kill during `BGSAVE`, restart, verify the previous snapshot is used and no data is lost.
- Truncate the AOF mid-record by hand, restart, verify replay stops cleanly at the last valid record and the server starts.

**Expiration tests.**
- Set a TTL, wait past it, verify the key is absent and not counted by `KEYS` or `EXISTS`.
- Set a TTL, `BGSAVE`, restart after a delay exceeding the TTL, verify the key is absent. This is the Section 6.3 correctness argument as an executable test.
- Set a TTL, `BGSAVE`, restart after a delay shorter than the TTL, verify the reported TTL has decreased by approximately the offline interval.
- Set many keys with TTLs, never access them, verify the active sweep reclaims them.

**Concurrency tests.**
- 100 simultaneous clients issuing interleaved writes to disjoint keys; verify every write is present.
- 100 simultaneous clients issuing `INCR` to a single shared key N times each; verify the final value is exactly the product. This is the test that would expose a lost update if the single-threaded atomicity claim of Section 6.1 were violated.
- Pipelined batches from multiple clients; verify per-client reply ordering is preserved.

## 8. Evaluation Methodology

A single throughput number is not a result.
The evaluation is a controlled comparison against official Redis under identical conditions.

**Controlled conditions.**
Both servers are measured on the same machine, in the same session, with the reported CPU model and core count, RAM, OS and kernel version, Go version, and official Redis version.
Both bind to loopback to remove network variability.
Persistence configuration is stated per scenario and matched across servers.
Each scenario runs three times after a discarded warm-up run, and the median is reported alongside the spread.
The keyspace is flushed between runs.

**Metrics captured per scenario.**
Requests per second; average latency; p50, p95, p99, and p99.9 latency; peak CPU utilisation; and peak resident memory.

**Scenarios.**

| # | Scenario | Purpose |
|---|----------|---------|
| 1 | Pure `GET`, 64-byte values, 50 clients, no pipelining | Read path baseline |
| 2 | Pure `SET`, 64-byte values, 50 clients, no pipelining | Write path baseline including AOF cost |
| 3 | Mixed 80% `GET` / 20% `SET`, 64-byte values, 50 clients | Realistic cache workload |
| 4 | Pure `SET`, 4 KB values, 50 clients | Isolates payload size and allocator behaviour from per-command overhead |
| 5 | Pure `SET`, 64-byte values, pipeline depth 16, and a client-count sweep at 1, 10, 50, and 200 clients | Isolates per-request syscall overhead from per-command cost, and locates the point where the single-threaded loop saturates |

Scenario 5 additionally repeats at fsync policy `always` versus `everysec` to quantify the durability trade-off of Section 6.4.
A supplementary measurement records event-loop stall time during `KEYS *` against keyspaces of 10^4, 10^5, and 10^6 keys, quantifying the Section 6.2 trade-off.

**Analysis requirement.**
The report attributes each observed gap to a specific architectural decision, not to general slowness.
Candidate attributions to test include Go's garbage collector versus manual memory management, the absence of a specialised small-object allocator, the absence of compact encodings such as listpack for small collections, and per-command allocation in the RESP codec.

## 9. Required Diagrams

The final report contains at minimum:

1. **System architecture.** Client through TCP, event loop, RESP parser, dispatcher, data store, and the fan-out to expiry, AOF, and snapshot subsystems.
2. **Persistence flow.** Write path through the store and AOF, `BGSAVE` temp-file-and-rename sequence, and the startup recovery sequence of Section 6.4.
3. **Replication flow.** Handshake, initial state transfer, and steady-state propagation from primary to replicas (Stretch; drawn only if S1 survives).
4. **Request lifecycle.** A single command traced from socket read through parse, dispatch, store mutation, AOF append, and reply serialisation.

## 10. Success Criteria

The project is considered successful when all of the following hold simultaneously.

1. An unmodified `redis-cli` performs a full session against the server covering every Must-Have command.
2. The server is killed with `SIGKILL` and restarted, and the complete keyspace including correctly aged TTLs is recovered.
3. All four crash-recovery tests of Section 7 pass, including the crash-during-`BGSAVE` and torn-AOF-record cases.
4. All five evaluation scenarios of Section 8 complete against both this server and official Redis, with the comparison table and the attribution analysis written up.
5. The full test suite passes from a clean checkout with a single command.
6. Every major module has at least two team members who can explain it under questioning, per Section 12.
7. The team can defend at least one deliberate design divergence from Redis, and can state what the single-threaded model costs in measured terms.

## 11. Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Team is new to Go and to systems programming | High | Weeks 1 and 2 are shared onboarding; nobody works alone until the core is built together |
| Knowledge silos leave one member unable to explain a module at viva | High | Every module has a named secondary reviewer who co-signs its pull requests; see Section 12 |
| A single member stalls and blocks the others | High | The `Store` interface and handler signature are frozen in Week 2, so tracks progress independently |
| Hard items deferred to the end and never finished | High | Skiplist, AOF rewrite, and replication handshake are all Stretch, not Must-Have |
| Evaluation compressed into the final week and reduced to one number | High | Section 8 fixes the methodology now; Week 12 is reserved for it and cannot be encroached upon |
| Scope creep into cluster mode or extra data types | Medium | Section 4.4 declares these out of scope in writing, before code |
| Integration between tracks fails late | Medium | Weekly live demo against `redis-cli`; merges to `main` at least weekly |

## 12. Team, Ownership, and Cross-Review

Every major module has a **primary** who implements it and a **secondary** who reviews every pull request against it and must be able to explain it without the primary present.
No pull request merges without the secondary's approval.
This exists specifically to prevent the failure mode where a viva question about persistence can only be answered by one person.

| Module | Primary | Secondary reviewer |
|--------|---------|--------------------|
| Core engine, expiry, transactions | Vighnesh | Rajat Yadav |
| Data structures: lists, hashes, sets, sorted sets | Ritk Kumar | Vighnesh |
| Persistence: AOF, snapshots, recovery | Rajat Yadav | Ritk Kumar |
| Protocol codec, pipelining, Pub/Sub | Vivek Sharma | Vighnesh |
| Crash-recovery tests, test runner, Docker tooling | Vivek Sharma | Rajat Yadav |
| Evaluation and benchmarking | Ritk Kumar | Rajat Yadav |
| Replication (Stretch) | Vighnesh | Ritk Kumar |

At each weekly sync, one module is selected at random and its **secondary**, not its primary, presents it.
This is viva rehearsal and it starts in Week 3, not Week 14.

Detailed week-by-week assignments are in [IMPLEMENTATION-PLAN.md](IMPLEMENTATION-PLAN.md).

## 13. Final Demonstration Script

The demonstration is rehearsed end to end at least twice before evaluation.

```
$ ./redis-from-scratch --port 6380
$ redis-cli -p 6380
```

1. **Basic keyspace.** `SET name Vighnesh`, `GET name`, `EXISTS name`, `DEL name`.
2. **Expiry.** `SET session abc`, `EXPIRE session 10`, `TTL session`, wait, `GET session` returns nil.
3. **Collections.** `LPUSH users Vighnesh`, `LPUSH users Rajat`, `LRANGE users 0 -1`, `HSET`, `HGETALL`, `SADD`, `SINTER`.
4. **Persistence.** `BGSAVE`, `SET k v`, kill the server with `SIGKILL`, restart, `GET k` and `TTL session` both correct. This demonstrates snapshot plus log tail and correctly aged TTL in one step.
5. **Evaluation.** Present the Section 8 comparison table and charts against official Redis, and state the attribution for the largest gap.
6. **Replication (Stretch).** One primary and two replicas; write on the primary, read on both replicas, and show a write to a replica rejected.
