# Implementation Plan

Companion to [PROPOSAL.md](PROPOSAL.md).
The Must-Have and Stretch cut line defined in Section 4 of the proposal governs everything below.
Where this plan and the cut line disagree, the cut line wins.

## Ground Rules

1. One repository, one `main` branch,   feature branches merged via pull request.
2. Every pull request needs two approvals: the lead, and the module's named **secondary reviewer** from Proposal Section 12. The secondary's approval means "I can explain this without the author present", not "the diff looks fine".
3. `redis-cli` compatibility is the definition of done for every command.
4. Every command ships with tests in the same pull request, asserting invariants per Proposal Section 7, not just happy-path behaviour.
5. Weekly 30-minute sync. One module is picked at random and its **secondary** presents it. Viva rehearsal starts Week 3.
6. No member starts a Stretch item until all of their Must-Have items are merged.
7. Absolute expiration timestamps are used everywhere a TTL is stored or logged, per Proposal Section 6.3. No code path persists a relative duration.

## Phase 0: Shared Foundation (Weeks 1 to 2)

All four members work together. Nobody works alone in this phase.

**Learning.** Watch the RESP protocol, TCP server, and event loop videos from the Redis Internals playlist as a group. Each member then independently writes a toy TCP echo server in Go to prove the basics landed.

**Build together.**

- RESP2 serializer and deserializer covering simple strings, errors, integers, bulk strings, and arrays.
- Non-blocking TCP listener and event loop.
- Command dispatcher and the command handler signature.
- `PING` and `ECHO` as the first two commands end to end.
- The write sequence-number counter, since both AOF and snapshot depend on it from day one (Proposal Section 6.4).

**Freeze at the end of Week 2.** The `Store` interface, the command handler signature, and the sequence-number contract are locked. Changing any of them afterwards requires agreement from all four members, because every track depends on them.

**Also produced in Week 2.** A first draft of the system architecture diagram and the request lifecycle diagram (Proposal Section 9, items 1 and 4). Drawing them now forces the interface freeze to be real, and they get refined rather than invented in Week 14.

**Exit criteria.** `redis-cli PING` returns `PONG` against the server, and all four members have this running locally.

## Phase 1: Parallel Tracks (Weeks 3 to 8)

### Vighnesh: Core engine, expiry, transactions

Secondary reviewer: Rajat Yadav.

| Weeks | Work | Cut line |
|-------|------|----------|
| 3 to 4 | Keyspace store behind the frozen interface; `SET`, `GET`, `DEL`, `EXISTS`, `INCR`, `KEYS` | M3 |
| 5 to 6 | Absolute-timestamp TTL storage, `EXPIRE`, `TTL`, `PERSIST`, passive expiry on lookup, active background sweep | M4 |
| 6 | Concurrency test: 100 clients issuing `INCR` to one key, asserting exact final value | M9 |
| 7 | `INFO`, `COMMAND`, server stats including torn-AOF-record reporting | M11 |
| 8 | `MULTI`, `EXEC`, `DISCARD` | S4, stretch |

Vighnesh also carries continuous responsibility for code review, merges, and unblocking the other three.
Budget roughly one third of the lead's time for this and do not treat it as overhead.

### Ritk Kumar: Data structures, then evaluation

Secondary reviewer: Vighnesh.

| Weeks | Work | Cut line |
|-------|------|----------|
| 3 to 4 | Lists: `LPUSH`, `RPUSH`, `LPOP`, `RPOP`, `LRANGE`, `LLEN`, plus mixed-end ordering invariant tests | M5 |
| 5 | Hashes: `HSET`, `HGET`, `HGETALL`, `HDEL`, `HLEN`, including the zero-fields-key-disappears invariant | M5 |
| 6 | Sets: `SADD`, `SREM`, `SMEMBERS`, `SISMEMBER`, `SINTER` | M6 |
| 7 | Benchmark harness: scripted runner for the five scenarios, machine-spec capture, three-runs-plus-warmup, median and spread reporting | M10 |
| 8 | Sorted sets with skiplist: `ZADD`, `ZSCORE`, `ZRANGE`, `ZRANK` | S2, stretch |

Building the benchmark harness in Week 7 rather than Week 12 is deliberate.
It means the team sees performance regressions while there is still time to act on them, and Week 12 becomes a measurement week rather than a tooling week.

The skiplist is the hardest single item on this track.
It is Stretch precisely so that a struggle there cannot sink the project.

### Rajat Yadav: Persistence

Secondary reviewer: Ritk Kumar.

| Weeks | Work | Cut line |
|-------|------|----------|
| 3 to 4 | AOF: length-prefixed checksummed records, configurable fsync policy, torn-record truncation on replay, `PEXPIREAT`-style rewriting of `EXPIRE` | M7 |
| 5 to 6 | Binary snapshot format with logical sequence boundary; `BGSAVE` via temp file plus atomic rename; boot from snapshot plus log tail; orphaned-temp cleanup | M8 |
| 7 | Recovery hardening: fix whatever Vivek's crash-recovery tests expose | M9 |
| 8 | AOF rewrite and compaction while writes continue | S3, stretch |

This is still the heaviest Must-Have track, which is why it now holds persistence alone.
If Rajat is behind at the Week 6 sync, Vivek drops pipelining and Pub/Sub and takes over the snapshot format, and Ritk stays on data structures.
Making this reassignment early is correct, not a failure.

### Vivek Sharma: Protocol hardening, recovery testing, tooling

Secondary reviewer: Vighnesh for protocol, Rajat Yadav for recovery tests.

| Weeks | Work | Cut line |
|-------|------|----------|
| 3 | RESP2 edge cases: inline commands, null bulk strings, malformed input, protocol errors, `HELLO 3` rejection | M1 |
| 4 | One-command test runner from a clean checkout, and CI running it on every pull request | M9 |
| 5 to 6 | The four crash-recovery tests of Proposal Section 7, including kill-during-`BGSAVE` and hand-truncated AOF, written against Rajat's persistence as it lands | M9 |
| 7 | Docker Compose file for the demo topology; server startup and flag handling | M11 |
| 8 | Pipelining, then Pub/Sub if time allows | S5, stretch |

Vivek writes the crash-recovery tests without reading Rajat's implementation first, working from the proposal's recovery guarantees alone.
That keeps the tests independent of the code they check, and it means the Week 8 checkpoint has a tester who did not write the persistence layer.

## Phase 2: Replication (Weeks 9 to 11)

Entirely Stretch (S1).
Start only if every Must-Have item from Phase 1 is merged into `main` at the Week 8 checkpoint.
If any Must-Have item is outstanding, all four members work on that item instead and replication is cut.

If it proceeds, all four work on it together:

- **Vighnesh:** `REPLICAOF` command, primary-replica handshake, replication state machine.
- **Rajat Yadav:** write propagation from primary to replicas, riding on the existing AOF sequence numbering.
- **Ritk Kumar:** replica-side apply loop and read-only enforcement on replicas.
- **Vivek Sharma:** testing with one primary plus two replicas, the Docker Compose topology for it, and the replication flow diagram.

**Exit criteria.** A write on the primary is visible on both replicas, and a write attempted directly on a replica is rejected.
The report states plainly that this provides redundancy and read scaling, not automatic failover, and that promoting a replica after primary loss is a manual operation.

## Phase 3: Evaluation and Delivery (Weeks 12 to 14)

All four members. Nothing here is optional. This phase is where the grade is earned, and no earlier phase may encroach on it.

**Week 12: evaluation (M10).**
Run all five scenarios of Proposal Section 8 against this server and official Redis under the controlled conditions stated there.
Capture throughput, p50, p95, p99, p99.9, CPU, and memory for each.
Run the fsync `always` versus `everysec` comparison and the `KEYS` event-loop stall measurement at 10^4, 10^5, and 10^6 keys.
Produce the comparison tables and charts.
Ritk leads, having built the harness in Week 7; all four interpret the results together.

**Week 13: hardening and packaging.**
Full test suite green from a clean checkout with the one-command runner Vivek built in Week 4.
Docker Compose file from Week 7 verified against the final binary.
Fix whatever the evaluation exposed, or document it as a known limitation if the fix is out of scope.

**Week 14: documentation and defence (M11).**
README with build and run instructions.
All four diagrams of Proposal Section 9 finalised.
The attribution analysis: for each measured gap, the specific architectural decision responsible, argued rather than asserted.
Future work covering cluster mode, `SCAN`, RESP3, and automatic failover.
Rehearse the Section 13 demonstration script end to end at least twice, with the **secondary** of each module fielding questions on it.

## Checkpoints

| Week | Gate | Action if failed |
|------|------|------------------|
| 2 | `redis-cli PING` works; interfaces and sequence-number contract frozen | Do not start Phase 1. Extend Phase 0 by one week. |
| 6 | M1, M3, M4, M5, M7 merged; every merged module has a secondary who presented it at a sync | Reassign Rajat's remaining work as described above. |
| 8 | All Must-Have items except M10 and M11 merged; benchmark harness runs | Cut replication entirely. All four finish Must-Have items. |
| 11 | Replication demo works, or was cut cleanly | Proceed to Phase 3 regardless. Phase 3 never slips. |
| 14 | All seven success criteria in Proposal Section 10 hold | Ship what exists and document the gap honestly in the report. |

## The Three Hard Parts

Named here so nobody is surprised by them in Week 12.

1. **Skiplist for sorted sets.** Probabilistic level assignment and correct range queries are easy to get subtly wrong.
2. **AOF rewrite and compaction.** Correctness while writes continue during the rewrite is the tricky part.
3. **Replication handshake.** A state machine with more edge cases than it appears to have from the outside.

All three are Stretch goals.
Attempt them in Phase 1 and Phase 2 while there is still schedule left to absorb a failure, never later.

## Viva Preparation Ledger

Maintained from Week 3, not written in Week 14.
For each question below, the ledger records the answer and which two members can give it.

- Why single-threaded, and what does it cost in measured terms?
- Why absolute expiration timestamps rather than remaining TTL?
- What happens on a crash during `BGSAVE`?
- What happens to a partially written AOF record?
- If the snapshot and the AOF disagree, which wins, and why can they not actually disagree?
- Why is `KEYS` a supported command in a project that emphasises non-blocking service?
- What does replication give you, and what does it explicitly not give you?
- Where is this implementation slower than Redis, and which specific design decision causes it?

The answer to every one of these is already in the proposal.
The ledger exists to confirm two people can say it aloud.
