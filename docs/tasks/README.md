# Task Board

Each file here is one task sized for a single sitting, with everything needed to do it without reading the whole plan first.
Pick a task whose dependencies are merged, put your name in the Status column, open a branch named after the task id, and follow the file.
Merge rules are in [MASTER-PLAN.md](../MASTER-PLAN.md) Section 1.3.

Owners are suggestions from the implementation plan. Anyone may take any task, but a Stretch task (T9.x) may only be started by someone whose Must items are all merged.

## Phase 0: Foundation (Weeks 1 to 2, everyone)

| Task | Title | Item | Depends on | Effort | Status |
|---|---|---|---|---|---|
| [T0.01](t0-01-repo-skeleton-ci.md) | Repository skeleton, Makefile and CI | M9, M11 | none | 2 to 3 h | Merged (PR #72) |
| [T0.02](t0-02-resp-reader.md) | RESP2 reader | M1 | T0.01 | 3 to 4 h | Merged (PR #73) |
| [T0.03](t0-03-resp-writer.md) | RESP2 writer and reply types | M1 | T0.01 | 2 h | Merged (PR #74) |
| [T0.04](t0-04-epoll-listener.md) | Epoll listener and connection registry | M2 | T0.01 | 5 to 6 h | Merged (PR #75) |
| [T0.05](t0-05-buffers-pipelining.md) | Buffers, incremental parsing, pipelining | M1, M2 | T0.02, T0.04 | 3 to 4 h | Merged (PR #76) |
| [T0.06](t0-06-dispatcher-ping-echo.md) | Command table, dispatcher, PING, ECHO, QUIT | M1 | T0.03, T0.05 | 2 to 3 h | Merged (PR #77) |
| [T0.07](t0-07-store-types.md) | Store types, Entry, Value, Clock, map store | M3 | T0.01 | 3 h | |
| [T0.08](t0-08-seq-and-propagate.md) | Sequence counter and Propagate hook | M7, M8 | T0.06, T0.07 | 2 h | |
| [T0.09](t0-09-tooling-smoke-test.md) | Client tooling smoke test | M1 | T0.06 | 1 to 2 h | |
| [T0.10](t0-10-decisions-and-diagram-drafts.md) | DECISIONS.md and diagram drafts | M11 | none | 3 h | |
| [T0.11](t0-11-contracts-freeze.md) | Contracts freeze | all | T0.02 to T0.08 | 1 h + meeting | |

## Phase 1: Parallel tracks (Weeks 3 to 8)

### Vivek: protocol hardening, test harness, crash tests

| Task | Title | Item | Depends on | Effort | Status |
|---|---|---|---|---|---|
| [T1.01](t1-01-resp-edge-cases.md) | RESP2 edge cases and protocol errors | M1 | T0.05, T0.06 | 3 h | |
| [T1.02](t1-02-testutil-server-process.md) | Test harness: ServerProcess | M9 | T0.01 | 3 h | |
| [T1.03](t1-03-testutil-resp-client.md) | Test harness: RESP client | M9 | T0.02, T0.03 | 2 h | |
| [T1.04](t1-04-hundred-connections-test.md) | 100-connection goroutine-count test | M2 | T1.02, T1.03 | 2 h | |
| [T1.05](t1-05-make-test-and-ci.md) | One-command test runner and CI | M9 | T0.01, T1.02 | 1 to 2 h | |
| [T7.01](t7-01-crash-tests-cr1-cr2.md) | Crash tests CR1 and CR2 | M9 | T1.02, T1.03 | 2 to 3 h | |
| [T7.02](t7-02-crash-test-cr3-kill-during-bgsave.md) | Crash test CR3: kill during BGSAVE | M9 | T7.01, T6.03 | 2 h | |
| [T7.03](t7-03-crash-test-cr4-torn-record.md) | Crash test CR4: torn record | M9 | T7.01 | 1 to 2 h | |
| [T7.04](t7-04-expiry-across-restart-tests.md) | Expiry tests EX1 to EX3 | M9 | T7.01, T3.01 | 1 to 2 h | |
| [T7.05](t7-05-concurrency-tests-cc1-cc2.md) | Concurrency tests CC1 and CC2 | M9 | T1.03 | 1 to 2 h | |
| [T11.01](t11-01-docker.md) | Dockerfile and Docker Compose | M11 | T0.01 | 2 h | |

### Vighnesh: core engine and expiry

| Task | Title | Item | Depends on | Effort | Status |
|---|---|---|---|---|---|
| [T2.01](t2-01-set-get-del-exists.md) | SET, GET, DEL, EXISTS | M3 | T0.06 to T0.08 | 2 to 3 h | |
| [T2.02](t2-02-incr.md) | INCR | M3 | T2.01 | 1 to 2 h | |
| [T2.03](t2-03-keys.md) | KEYS | M3 | T0.07 | 1 to 2 h | |
| [T2.04](t2-04-strings-cli-session-test.md) | redis-cli session test for strings | M3 | T2.01 to T2.03 | 1 h | |
| [T3.01](t3-01-expire-ttl-persist.md) | Expiry storage, EXPIRE, TTL, PERSIST, passive | M4 | T0.07, T0.08, T2.01 | 3 to 4 h | |
| [T3.02](t3-02-active-sweep.md) | Active expiry sweep | M4 | T3.01, T0.04 | 2 to 3 h | |
| [T3.03](t3-03-expiry-unit-tests.md) | Expiry invariant tests | M9 | T3.01, T3.02 | 2 h | |
| [T3.04](t3-04-incr-lost-update-test.md) | Shared-key INCR test (CC3) | M9 | T2.02, T1.03 | 1 h | |
| [T3.05](t3-05-info.md) | INFO | M11 | T0.06, T0.08 | 2 h | |
| [T3.06](t3-06-command-and-counters.md) | COMMAND and stats counters | M11 | T0.06 | 1 to 2 h | |

### Ritk: data structures, then benchmark harness

| Task | Title | Item | Depends on | Effort | Status |
|---|---|---|---|---|---|
| [T4.01](t4-01-list-deque.md) | ListVal deque | M5 | T0.07 | 2 to 3 h | |
| [T4.02](t4-02-list-commands.md) | List commands | M5 | T4.01, T0.06, T0.08 | 3 h | |
| [T4.03](t4-03-list-property-test.md) | List mixed-end property test | M9 | T4.02 | 1 to 2 h | |
| [T4.04](t4-04-hash-commands.md) | Hash commands | M5 | T0.07, T0.06, T0.08 | 2 to 3 h | |
| [T4.05](t4-05-set-commands.md) | Set commands | M6 | T0.07, T0.06, T0.08 | 2 to 3 h | |
| [T4.06](t4-06-hash-set-invariant-tests.md) | Hash and set invariant tests | M9 | T4.04, T4.05 | 1 to 2 h | |
| [T8.01](t8-01-bench-runner.md) | Benchmark runner and scenario config | M10 | T1.05 | 4 h | |
| [T8.02](t8-02-bench-sampler.md) | CPU and memory sampler | M10 | none | 1 to 2 h | |
| [T8.03](t8-03-bench-spec-capture.md) | Machine specification capture | M10 | none | 30 min | |
| [T8.04](t8-04-bench-report-tool.md) | Report tool: medians and tables | M10 | T8.01, T8.02 | 3 h | |
| [T8.05](t8-05-keys-stall-measurement.md) | KEYS stall measurement | M10 | T2.03, T8.01 | 2 h | |
| [T8.06](t8-06-bench-charts.md) | Charts | M10 | T8.04 | 2 h | |

### Rajat: persistence

| Task | Title | Item | Depends on | Effort | Status |
|---|---|---|---|---|---|
| [T5.01](t5-01-aof-record-codec.md) | Log record codec | M7 | T0.01, T0.03 | 2 to 3 h | |
| [T5.02](t5-02-aof-writer-fsync.md) | Log writer with fsync policies | M7 | T5.01, T0.08 | 3 h | |
| [T5.03](t5-03-aof-replay-truncation.md) | Replay and torn-record truncation | M7 | T5.01, T5.02, T0.06 | 3 h | |
| [T5.04](t5-04-startup-log-recovery.md) | Startup: replay and resume sequence | M7 | T5.03 | 1 to 2 h | |
| [T6.01](t6-01-snapshot-codec.md) | Snapshot codec | M8 | T0.07, T4.02, T4.04, T4.05 | 3 h | |
| [T6.02](t6-02-store-snapshot-cow.md) | Store.Snapshot clone and copy-on-write | M8 | T0.07, T4.02 | 2 to 3 h | |
| [T6.03](t6-03-bgsave.md) | BGSAVE, writer goroutine, atomic rename | M8 | T6.01, T6.02, T0.04 | 3 to 4 h | |
| [T6.04](t6-04-startup-recovery-sequence.md) | Full startup recovery | M8 | T5.04, T6.01, T6.03 | 2 h | |

## Stretch (Week 8 and Weeks 9 to 11, cut-line rules apply)

| Task | Title | Item | Depends on | Effort | Status |
|---|---|---|---|---|---|
| [T9.01](t9-01-skiplist.md) | Skiplist | S2 | T0.07 | 4 to 5 h | |
| [T9.02](t9-02-zset-commands.md) | Sorted set commands and persistence | S2 | T9.01, T6.01 | 3 h | |
| [T9.03](t9-03-transactions.md) | MULTI, EXEC, DISCARD | S4 | T0.06, T0.08 | 3 h | |
| [T9.04](t9-04-pubsub.md) | SUBSCRIBE, UNSUBSCRIBE, PUBLISH | S5 | T0.05, T0.06 | 3 h | |
| [T9.05](t9-05-log-compaction.md) | Log compaction after BGSAVE | S3 | T6.03, T6.04 | 4 h | |
| [T9.06](t9-06-replicaof-handshake.md) | REPLICAOF, state machine, handshake | S1 | Week 8 gate, T6.01, T5.01 | 6 h | |
| [T9.07](t9-07-primary-fanout.md) | Primary side: replica links and fan-out | S1 | T9.06, T0.08, T6.03 | 4 h | |
| [T9.08](t9-08-replica-apply-readonly.md) | Replica side: apply and read-only | S1 | T9.06 | 3 h | |
| [T9.09](t9-09-replication-topology-test.md) | Topology, end-to-end test, diagram | S1 | T9.06 to T9.08, T11.01 | 3 h | |

## Phase 3: Evaluation and delivery (Weeks 12 to 14, everyone)

| Task | Title | Item | Depends on | Effort | Status |
|---|---|---|---|---|---|
| [T10.01](t10-01-baseline-and-spec.md) | Install baseline and capture spec | M10 | T8.03 | 1 to 2 h | |
| [T10.02](t10-02-run-scenarios-1-to-4.md) | Run Scenarios 1 to 4 | M10 | T10.01, T8.01 | 3 to 4 h | |
| [T10.03](t10-03-run-scenario-5-and-fsync.md) | Run Scenario 5 and fsync comparison | M10 | T10.01, T8.01 | 3 h | |
| [T10.04](t10-04-run-keys-stall.md) | Run KEYS stall measurement | M10 | T8.05, T10.01 | 2 h | |
| [T10.05](t10-05-attribution-experiments.md) | Attribution experiments | M10 | T10.02, T10.03 | 3 to 4 h | |
| [T10.06](t10-06-write-evaluation-chapter.md) | Write the evaluation chapter | M10 | T10.02 to T10.05 | 4 h | |
| [T11.02](t11-02-readme.md) | README | M11 | all merged | 2 h | |
| [T11.03](t11-03-final-diagrams.md) | Final diagrams | M11 | T0.10, T9.09 | 2 to 3 h | |
| [T11.04](t11-04-final-report.md) | Final report assembly | M11 | T10.06, T11.03 | 6 h | |
| [T11.05](t11-05-viva-ledger-and-rehearsals.md) | Viva ledger and rehearsals | M11 | none | ongoing | |

## Critical path

T0.01 to T0.04 to T0.05 to T0.06 to T0.08 to T5.02 to T5.03 to T6.03 to T6.04 to T7.02 to T10.02.
Delays on that chain move the Week 8 checkpoint; everything else has slack.
