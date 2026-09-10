# Master Plan: Development and Implementation

Companion to [PROPOSAL.md](PROPOSAL.md), [IMPLEMENTATION-PLAN.md](IMPLEMENTATION-PLAN.md) and the submitted synopsis (`Synopsis_RedForge.pdf`).
The proposal defines *what* and *why*, the implementation plan defines *who* and *when*, and the synopsis is the version the department holds us to.
This document defines *how*: the repository layout, the contracts frozen at the end of Week 2, the on-disk formats, the pull-request sequence for every milestone, the test plan, the benchmark harness, and the exact definition of done for each item.

Precedence when documents disagree: synopsis, then proposal cut line (Section 4), then implementation plan, then this document.
Section 2 lists every place this plan had to resolve a gap between the other three, so nothing here is a silent change.

## 1. Scope Lock

Everything in this plan traces to a numbered item in the proposal and an objective in the synopsis.
Anything that does not trace is out of scope and is not built.

### 1.1 Traceability matrix

| Synopsis objective | Proposal item | Milestone in this plan | Verified by |
|---|---|---|---|
| Obj 1: RESP2 codec, single-threaded non-blocking loop, unmodified redis-cli | M1, M2 | MS0, MS1 | redis-cli session, 100-client test |
| Obj 2: strings, expiry, lists, hashes, sets with absolute timestamps | M3, M4, M5, M6 | MS2, MS3, MS4 | invariant tests, expiry-across-restart tests |
| Obj 3: hybrid persistence with checksummed sequenced log and atomic snapshots | M7, M8 | MS5, MS6 | four crash-recovery tests |
| Obj 4: one-command correctness suite with SIGKILL, expiry and 100-client cases | M9 | MS1, MS7 | CI green from clean checkout |
| Obj 5: five-scenario evaluation against official Redis with attribution | M10 | MS8, MS10 | comparison tables, charts, attribution text |
| Obj 6: extensions in priority order | S1 to S5 | MS9 | per-extension tests |
| Deliverable 6: README, diagrams, Docker Compose, final report | M11 | MS11 | reviewer checklist |

### 1.2 Command set is closed

The only commands the server will ever accept are those in Table 3.1 of the synopsis (Proposal Section 5).
The full list, for grep purposes:

```
PING ECHO INFO COMMAND BGSAVE
SET GET DEL EXISTS INCR KEYS
EXPIRE TTL PERSIST
LPUSH RPUSH LPOP RPOP LLEN LRANGE
HSET HGET HDEL HLEN HGETALL
SADD SREM SISMEMBER SMEMBERS SINTER
-- stretch --
ZADD ZSCORE ZRANK ZRANGE
MULTI EXEC DISCARD
SUBSCRIBE UNSUBSCRIBE PUBLISH
REPLICAOF
```

Two internal-only additions are needed for the design to work and are not user-facing features:

- `PEXPIREAT` exists only as the rewritten form of `EXPIRE` inside the log and inside replication streams. It is accepted from a client too, because rejecting a record format the server itself writes would be strange, but it is not advertised and not in the demo.
- `QUIT` is accepted because redis-cli sends it on exit. It closes the connection and touches no state.

Every other command returns `-ERR unknown command 'x'`.
`HELLO` returns `-ERR unknown command` so that RESP3 negotiation fails cleanly, as the synopsis requires.

### 1.3 Scope check on every pull request

The pull-request template carries this checklist and the secondary reviewer refuses to approve without it:

- [ ] Names the M or S item this PR advances.
- [ ] Adds no command outside Section 1.2.
- [ ] Adds no dependency outside the Go standard library.
- [ ] Persists no relative TTL anywhere (Proposal Section 6.3).
- [ ] Includes tests asserting an invariant, not just a happy path.
- [ ] Works against `redis-cli` and the PR description shows the session.
- [ ] Secondary reviewer can explain the change without the author.

## 2. Gaps Between the Documents and How This Plan Resolves Them

These were found while reconciling the four sources.
Each resolution stays inside the declared scope.

1. **Pipelining is scheduled as Week 8 stretch work but is required earlier.** Scenario 5 of the evaluation uses pipeline depth 16, and the M9 concurrency tests include pipelined batches. Pipelining is therefore a property of the incremental parser built in Phase 0, not a Week 8 feature. Vivek's Week 8 item becomes the pipelining ordering test plus Pub/Sub.
2. **`BGSAVE` is described as background, but Go cannot fork like Redis does.** This plan uses a shallow clone of the keyspace map plus copy-on-write of collection values for the duration of the snapshot. See Section 4.6. The clone is O(N) on the event loop and its duration is measured and reported.
3. **The methodology requires flushing the keyspace between benchmark runs, but `FLUSHALL` is not in scope.** The harness restarts the server with an empty persistence directory between runs. This also resets the log, which matches the "persistence configuration stated per scenario" rule.
4. **Scenario 3 (80/20 mixed) cannot be expressed as one `redis-benchmark` invocation.** The harness runs two `redis-benchmark` processes concurrently, 40 clients on `GET` and 10 on `SET`, and reports both streams. The report states this.
5. **`redis-cli` 7.x sends `COMMAND DOCS` on connect.** Our `COMMAND` handler answers `COMMAND` and `COMMAND COUNT` with real data and returns an error for other subcommands. `redis-cli` tolerates that error and proceeds. This must be verified in Phase 0, because it is the difference between passing and failing the M1 definition of done.
6. **`redis-benchmark` 7.x sends `CONFIG GET` on startup.** It prints a warning when that fails and continues. Verified in Phase 0 alongside item 5.
7. **S3 (log compaction) has no command in the matrix.** Compaction is triggered automatically after every successful `BGSAVE`, never by a client command. See Section 4.8.
8. **`HLEN` appears in the command matrix but not in the M5 definition-of-done row.** It is treated as part of M5.
9. **The synopsis numbers phases I to V, the implementation plan numbers them 0 to 3.** Mapping: I = Phase 0, II = Phase 1, III = Week 8 checkpoint, IV = Phase 2, V = Phase 3. This document uses the implementation plan numbering.

## 3. Technical Decisions Fixed Before Phase 0 Ends

Each decision is recorded in `docs/DECISIONS.md` as one short entry: context, decision, alternatives rejected, consequence.
That file is the source for viva answers on "why did you do it this way".

### 3.1 Event loop: raw `epoll` through the `syscall` package

The synopsis promises a single-threaded event loop over non-blocking sockets with no goroutine per connection.
Go's `net` package hides its poller and hands each connection to whoever calls `Accept`, which pushes toward one goroutine per connection.
To keep the promise literally, the server uses `syscall.Socket`, `Bind`, `Listen`, `Accept4`, `SetNonblock`, `EpollCreate1`, `EpollCtl` and `EpollWait` directly.
`syscall` is part of the standard library, so the "standard library only" requirement in Table 6.2 holds.
The loop goroutine calls `runtime.LockOSThread` so it is one OS thread.

Rejected: `net` package with goroutine per connection feeding one executor goroutine over a channel.
It preserves atomicity but breaks the M2 wording and adds a channel hop to every request that would muddy the evaluation.
If the epoll loop is not serving `redis-cli PING` by the end of Week 2, the checkpoint fails and Phase 0 is extended by one week, per the implementation plan.
The fallback is not adopted silently.

Linux only.
Table 6.2 lists Linux as the only operating system, so no portability layer is written.

### 3.2 What runs off the loop thread, and why it does not need a lock

Exactly three goroutines exist besides the loop, and none of them touches the live keyspace:

| Goroutine | Reads | Writes | Sync with loop |
|---|---|---|---|
| fsync ticker (`everysec` only) | the log file descriptor | nothing in memory | `Fsync` on an fd is safe concurrently with `Write` on the same fd |
| snapshot writer | the cloned map and immutable values | temp file | completion signalled through an `eventfd` registered in epoll |
| compaction copier (S3) | old log file | new log file | completion signalled the same way |

Replica links (S1) are ordinary epoll file descriptors on the loop, not goroutines.

### 3.3 Timers live inside the loop

`EpollWait` is called with a timeout equal to the time until the next tick.
The tick, every 100 ms, runs the active expiry sample, triggers the `everysec` fsync request, and checks for `BGSAVE` completion.
There is no `time.Ticker` goroutine touching state.

### 3.4 Clock is injected

All time reads go through a `Clock` interface with `NowMs() int64`.
Production uses the wall clock, as the synopsis states.
Unit tests use a fake clock so expiry tests are deterministic and fast.
Integration tests use the real clock with tolerances stated per test.

### 3.5 Configuration surface

```
--port        int     default 6380
--bind        string  default 127.0.0.1
--dir         string  default ./data      persistence directory
--appendfsync string  default everysec    always | everysec | no
--replicaof   string  default ""          "host port", stretch S1
--debug-bgsave-delay-ms int default 0     test hook, slows the snapshot writer
```

The debug flag exists so the kill-during-`BGSAVE` test is deterministic without a huge keyspace.
It is documented as a test hook and never used in the evaluation.

### 3.6 File names

`<dir>/appendonly.aof` and `<dir>/dump.snap`, matching Fig. 5.1.
Temporary snapshot files are `<dir>/dump.snap.tmp-<pid>`.

## 4. Frozen Contracts (end of Week 2)

Changing anything in this section after the freeze needs all four members to agree in writing on the pull request.

### 4.1 Repository layout

```
redis-from-scratch/
  cmd/redis-from-scratch/main.go     flags, recovery, serve
  internal/resp/                     RESP2 reader and writer            M1
  internal/server/                   epoll loop, connections, buffers   M2
  internal/command/                  dispatcher, command table, handlers
    strings.go keys.go expire.go lists.go hashes.go sets.go server.go
    zsets.go multi.go pubsub.go      stretch
  internal/store/                    keyspace, Entry, Value types, expiring index, clock  M3 to M6
  internal/store/expiry.go           passive lookup check and active sweep               M4
  internal/aof/                      record codec, writer, fsync policy, replay, truncation  M7
  internal/snapshot/                 format, writer, loader                               M8
  internal/recovery/                 startup sequence                                     M7, M8
  internal/replication/              stretch S1
  internal/testutil/                 test-only RESP client, server process harness
  test/integration/                  redis-cli sessions, crash-recovery, expiry, concurrency  M9
  bench/                             harness, machine-spec capture, result CSVs, charts   M10
  deploy/Dockerfile deploy/docker-compose.yml                                            M11
  docs/                              proposal, plans, diagrams, decisions, viva ledger, report
  Makefile  .github/workflows/ci.yml  go.mod
```

Module path is `github.com/<org>/redis-from-scratch`.
`go.mod` declares `go 1.22` and has no `require` block, ever.

### 4.2 Value model

```go
type Type uint8
const ( TString Type = iota + 1; TList; THash; TSet; TZSet )

type Entry struct {
    Type     Type
    Val      Value   // *StringVal, *ListVal, *HashVal, *SetVal, *ZSetVal
    ExpireAt int64   // absolute Unix ms, 0 = no expiry
}
```

`StringVal` wraps `[]byte` and is never mutated in place; `INCR` and `SET` replace it.
`ListVal` is a deque over a ring-buffered slice giving O(1) amortised push and pop at both ends.
`HashVal` and `SetVal` wrap Go maps.
`ZSetVal` (S2) is a skiplist plus a `map[string]float64` for O(1) score lookup.
Every `Value` implements `Clone() Value`, which the copy-on-write path in Section 4.6 needs.

### 4.3 Store interface

```go
type Store interface {
    // Lookup applies passive expiry: an expired key is deleted and reported absent.
    Lookup(key string) (*Entry, bool)
    // Mutable returns the entry for in-place mutation, cloning the value first
    // if a snapshot is in progress. Also applies passive expiry.
    Mutable(key string) (*Entry, bool)
    Put(key string, e *Entry)
    Delete(key string) bool
    Exists(key string) bool
    Keys(pattern string) []string           // glob per Redis: * ? [abc] \x
    SetExpireAt(key string, atMs int64) bool
    Persist(key string) bool                // true if a TTL was removed
    TTLms(key string) (remainingMs int64, status TTLStatus)  // NoKey, NoExpire, HasExpire
    Len() int
    ExpiringLen() int
    SweepExpired(sampleSize int, budget time.Duration) (deleted int)
    Snapshot() SnapshotView                 // shallow clone for the writer, see 4.6
    ForEach(fn func(key string, e *Entry) bool)
}
```

`Keys` matches the Redis glob rules so `redis-cli --scan`-free tooling and the tests behave identically on both servers.

### 4.4 Command handler signature and the propagate hook

```go
type Handler func(ctx *Ctx, args [][]byte) resp.Reply

type Ctx struct {
    Client *server.Client
    Store  store.Store
    Now    int64            // ms, read once per command
    Srv    *server.Server   // stats, INFO fields, bgsave state
}

// Propagate records the canonical write form of this command.
// The dispatcher appends it to the log with a fresh sequence number
// after the handler returns a non-error reply, and fans it out to replicas (S1).
func (c *Ctx) Propagate(args ...[]byte)

type Command struct {
    Name    string
    Arity   int      // negative means "at least"
    Flags   Flags    // Write | ReadOnly | Admin | PubSub | NoTx
    Handler Handler
}
```

Rules:

- Read commands never call `Propagate` and never consume a sequence number.
- A handler that changes state calls `Propagate` exactly once with the canonical form. `EXPIRE k 10` propagates `PEXPIREAT k <absolute ms>`. `INCR k` propagates `INCR k`, since replay re-derives the same value. `SET k v` propagates itself.
- A handler that fails, or that turns out to be a no-op such as `DEL` on a missing key, does not propagate.
- Deletions performed by the expiry sweep are not propagated. Replay and replicas drop expired keys from the absolute timestamp, so logging the deletion would add records without adding information. This is recorded in `DECISIONS.md`.

### 4.5 Sequence number contract

- `seq` is a `uint64`, starts at 0 on a fresh directory, and is incremented by exactly one per propagated write.
- It is assigned on the loop thread inside the dispatcher, never inside a handler.
- Every log record carries its `seq`. The snapshot carries `max_seq`, the highest `seq` whose effect it contains.
- On recovery the counter resumes from `max(snapshot.max_seq, last valid log record seq)`.
- Replay applies only records with `seq > snapshot.max_seq`, which is what makes the two files unable to disagree.
- Under S1 a replica applies records with the primary's `seq` and never assigns its own.

### 4.6 `BGSAVE` without fork

1. The handler rejects a second `BGSAVE` while one is running with `-ERR Background save already in progress`.
2. On the loop thread: record `boundary = seq`, call `Store.Snapshot()`, which copies the top-level `map[string]*Entry` into a new map and sets `snapshotActive = true`. Entries are shared, not copied. The duration of this copy is recorded in INFO as `last_bgsave_clone_ms`.
3. The snapshot goroutine serialises the cloned map to `dump.snap.tmp-<pid>`, `fsync`s the file, renames it over `dump.snap`, `fsync`s the directory, and writes to the `eventfd`.
4. While `snapshotActive`, `Store.Mutable` clones a collection value before returning it and stores the clone in the live map. The snapshot goroutine therefore only ever sees values that no handler will mutate. `Put` and `Delete` act on the live map only, which the goroutine never reads.
5. On the `eventfd` readiness the loop clears `snapshotActive`, drops its reference to the clone, and updates `last_bgsave_status`, `last_bgsave_seq` and `last_bgsave_time`.

A crash at any point before the rename leaves the previous `dump.snap` intact and a `dump.snap.tmp-*` orphan, which startup deletes.
This is exactly Proposal Section 6.4 and the sequence in Fig. 5.5.

### 4.7 Log record format

Little-endian throughout.

```
file header   "RFAOF" + version u8 (=1)                 written once on create
record        seq u64 | len u32 | crc32 u32 | payload[len]
payload       RESP2 array of the propagated command, e.g. *3\r\n$3\r\nSET\r\n...
crc32         IEEE, computed over seq || len || payload
```

Replay reads records sequentially.
A record whose header is truncated, whose payload is shorter than `len`, or whose CRC does not match terminates replay.
The file is then truncated to the byte offset where that record began, the event is logged, and INFO reports `aof_last_truncation_offset` and `aof_truncated_records` (always 0 or 1, since replay stops at the first bad record).

Fsync policy:

| Policy | Behaviour |
|---|---|
| `always` | `Write` then `Fsync` on the loop thread before the reply is queued |
| `everysec` | `Write` on the loop thread; the fsync goroutine calls `Fsync` once per second when the tick asks it to |
| `no` | `Write` only; the kernel decides |

### 4.8 Snapshot format

```
"RFSNAP" + version u8 (=1)
max_seq   u64
count     u64
entries   count times: key (u32 len + bytes) | type u8 | expire_at i64 | value
value     STRING: u32 len + bytes
          LIST:   u32 n, then n items (u32 len + bytes) head to tail
          HASH:   u32 n, then n pairs (field, value)
          SET:    u32 n, then n members
          ZSET:   u32 n, then n pairs (member, score f64)   stretch S2
crc32     u32 over everything before it
```

The loader verifies magic, version and CRC before touching the store.
A snapshot that fails verification is ignored with a logged warning and recovery proceeds from the log alone, because a corrupt snapshot must not take the server down.

Compaction (S3) after a successful rename with boundary N: the copier goroutine writes a new file `appendonly.aof.new` containing only records with `seq > N` from the old file, signals completion, and the loop then appends any records written since the copier finished, `fsync`s, renames `appendonly.aof.new` over `appendonly.aof`, and continues appending to the new descriptor.
The remainder copied on the loop thread is small because it covers only the window between the copier finishing and the loop noticing.

### 4.9 Startup recovery sequence

```
delete dump.snap.tmp-*
if dump.snap exists and verifies: load it, seq = max_seq
open appendonly.aof (create with header if missing)
replay records with seq > max_seq; stop and truncate at the first bad record
drop every key whose expire_at <= now; count them as expired_keys
seq = max(seq, last replayed seq)
start listening
```

### 4.10 INFO fields

INFO returns the Redis section format so `redis-cli INFO` and `redis-cli --stat` render it.

```
# Server        redis_version:0.1.0-rfs  uptime_in_seconds  tcp_port  process_id
# Clients       connected_clients
# Stats         total_connections_received  total_commands_processed  expired_keys
                keyspace_hits  keyspace_misses
# Persistence   aof_fsync_policy  aof_current_seq  aof_last_truncation_offset  aof_truncated_records
                bgsave_in_progress  last_bgsave_status  last_bgsave_seq  last_bgsave_clone_ms
# Replication   role  connected_replicas  master_host  master_port  replication_seq   (S1)
# Keyspace      db0:keys=N,expires=M
```

`redis_version` must be present because `redis-cli` reads it to decide on some behaviours.

### 4.11 Error strings

Handlers reuse Redis's wording so tests can assert on it and client tools behave identically:

```
ERR unknown command 'x', with args beginning with: ...
ERR wrong number of arguments for 'x' command
WRONGTYPE Operation against a key holding the wrong kind of value
ERR value is not an integer or out of range
ERR Background save already in progress
READONLY You can't write against a read only replica.      (S1)
EXECABORT Transaction discarded because of previous errors. (S4)
```

## 5. Milestones and Pull-Request Sequence

Weeks and owners come from the implementation plan.
Each pull request below is small enough to review in one sitting.
Dependencies are listed so nobody waits on an unmerged branch without knowing it.

### MS0: Foundation (Weeks 1 to 2, all four members)

| PR | Content | Depends on |
|---|---|---|
| P0.1 | Repo skeleton, `go.mod`, `Makefile` (`build`, `test`, `lint`, `bench`), CI workflow running `gofmt -l`, `go vet`, `go test ./...` | none |
| P0.2 | `internal/resp`: reader for the five RESP2 types and inline commands, writer for the five types, table-driven tests, fuzz test on the reader | P0.1 |
| P0.3 | `internal/server`: epoll loop, accept, per-connection read and write buffers, incremental parsing so multiple pipelined frames in one read are all executed in order, `EPOLLOUT` handling on partial writes, `QUIT` | P0.2 |
| P0.4 | `internal/command`: command table, dispatcher with arity and unknown-command errors, `PING`, `ECHO`, `Ctx.Propagate` plumbing that appends to an in-memory list until the log exists | P0.3 |
| P0.5 | `internal/store`: `Entry`, `Value` types, `Store` interface with the map-backed implementation, `Clock`, sequence counter | P0.1 |
| P0.6 | `docs/DECISIONS.md` with Section 3 entries, first drafts of the system architecture and request lifecycle diagrams as Graphviz sources in `docs/diagrams/` | none |
| P0.7 | Freeze PR: this section's contracts copied into `docs/CONTRACTS.md`, approved by all four | P0.2 to P0.5 |

Exit criteria, all on every member's machine:

- `redis-cli -p 6380 PING` prints `PONG`.
- `redis-cli -p 6380` interactive session starts without hanging, which proves the `COMMAND DOCS` handling in Section 2 item 5.
- `redis-benchmark -p 6380 -t ping -q` runs to completion, which proves the `CONFIG GET` handling in Section 2 item 6.
- `printf 'PING\r\nPING\r\n' | nc localhost 6380` returns two `+PONG`, which proves pipelining and inline commands.

### MS1: Loop hardening and the test runner (Weeks 3 to 4, Vivek)

| PR | Content | Item |
|---|---|---|
| P1.1 | RESP2 edge cases: null bulk, empty array, oversized length rejection, malformed prefix, `HELLO` rejection, protocol error closes the connection with `-ERR Protocol error` | M1 |
| P1.2 | `internal/testutil`: `ServerProcess` (build once, start with flags and temp dir, `Kill()` sends SIGKILL, `Restart()`), and a minimal RESP client for tests | M9 |
| P1.3 | 100-concurrent-client test asserting no goroutine growth via `runtime.NumGoroutine()` sampled through a debug endpoint in INFO (`goroutines` field, test builds only) | M2 |
| P1.4 | `make test` runs unit and integration from a clean checkout; CI installs `redis-tools` so `redis-cli` is available and runs the same target | M9 |

### MS2: Strings and keyspace (Weeks 3 to 4, Vighnesh)

| PR | Content | Item |
|---|---|---|
| P2.1 | `SET`, `GET`, `DEL` (multi-key), `EXISTS` (multi-key) | M3 |
| P2.2 | `INCR` with the integer parsing rules Redis uses, `WRONGTYPE` on non-string | M3 |
| P2.3 | `KEYS` with glob matching and its tests | M3 |
| P2.4 | redis-cli session test for every M3 command using `redis-cli --no-raw` output comparison | M3 |

### MS3: Expiry (Weeks 5 to 6, Vighnesh)

| PR | Content | Item |
|---|---|---|
| P3.1 | `ExpireAt` on `Entry`, expiring-key index, passive check inside `Lookup` and `Mutable`, `EXPIRE`, `TTL`, `PERSIST`, `PEXPIREAT` propagation form | M4 |
| P3.2 | Active sweep on the 100 ms tick: sample 20 keys, delete expired, repeat while more than 25 percent of the sample was expired, bounded at 1 ms per tick | M4 |
| P3.3 | Unit tests with the fake clock for every invariant in Proposal Section 7, and the "many keys never accessed are reclaimed" integration test | M9 |
| P3.4 | `INCR` lost-update test: 100 clients each sending `INCR` N times, final value equals 100 times N | M9 |
| P3.5 | `INFO`, `COMMAND`, `COMMAND COUNT`, stats counters | M11 |

### MS4: Lists, hashes, sets (Weeks 3 to 6, Ritk)

| PR | Content | Item |
|---|---|---|
| P4.1 | `ListVal` deque with its own unit tests including wraparound | M5 |
| P4.2 | `LPUSH`, `RPUSH`, `LPOP`, `RPOP`, `LLEN`, `LRANGE` with negative indices, key removed when the list empties | M5 |
| P4.3 | Property test: random mixed-end operation sequence against a slice-backed reference model | M9 |
| P4.4 | `HSET` (multi-field), `HGET`, `HDEL`, `HLEN`, `HGETALL`, key removed at zero fields | M5 |
| P4.5 | `SADD`, `SREM`, `SISMEMBER`, `SMEMBERS`, `SINTER` iterating the smallest set first, key removed at zero members | M6 |
| P4.6 | Invariant tests for hashes and sets per Proposal Section 7 | M9 |

### MS5: Append-only log (Weeks 3 to 4, Rajat)

| PR | Content | Item |
|---|---|---|
| P5.1 | Record codec with unit tests, including truncated and corrupt inputs | M7 |
| P5.2 | Writer with the three fsync policies, wired into the dispatcher's propagate path | M7 |
| P5.3 | Replay through the dispatcher in replay mode (no propagation, no reply), torn-record stop and truncate, INFO fields | M7 |
| P5.4 | Recovery from log alone: write, SIGKILL, restart, verify keyspace and TTLs | M7 |

### MS6: Snapshots and recovery (Weeks 5 to 7, Rajat)

| PR | Content | Item |
|---|---|---|
| P6.1 | Snapshot codec with round-trip tests for every type and for the CRC failure path | M8 |
| P6.2 | `Store.Snapshot()` clone, `snapshotActive` copy-on-write in `Mutable`, unit test proving a mutation during snapshot does not appear in the file | M8 |
| P6.3 | `BGSAVE` handler, writer goroutine, temp file and rename, `eventfd` completion, orphan cleanup | M8 |
| P6.4 | Startup sequence of Section 4.9, resume of the sequence counter | M8 |
| P6.5 | Recovery hardening from Vivek's tests in MS7 | M9 |

### MS7: Crash-recovery and expiry-across-restart tests (Weeks 5 to 6, Vivek)

Written from the proposal's guarantees, not from Rajat's code.

| Test | Steps | Pass condition |
|---|---|---|
| CR1 log only | write 1000 keys of mixed types with some TTLs, SIGKILL, restart | every key present with the right type and value, TTLs aged |
| CR2 snapshot plus tail | write, `BGSAVE`, wait for `bgsave_in_progress:0`, write more, SIGKILL, restart | both halves present, INFO shows `last_bgsave_seq` below `aof_current_seq` |
| CR3 kill during BGSAVE | start with `--debug-bgsave-delay-ms 2000`, write, `BGSAVE`, SIGKILL after 500 ms, restart | previous `dump.snap` untouched or absent, no `dump.snap.tmp-*` left, all data present |
| CR4 torn record | write, SIGKILL, truncate the file by 7 bytes, restart | server starts, INFO reports one truncated record, every key before the cut is present |
| EX1 expired offline | `SET`, `EXPIRE 2`, `BGSAVE`, kill, sleep 3, restart | key absent from `GET`, `EXISTS` and `KEYS` |
| EX2 aged offline | `SET`, `EXPIRE 100`, `BGSAVE`, kill, sleep 5, restart | `TTL` between 93 and 96 |
| EX3 passive | `SET`, `EXPIRE 1`, sleep 2, `GET` | nil, and `expired_keys` incremented |
| EX4 active | 10000 keys with 1 second TTL, no access, sleep 5 | `db0:keys=0` in INFO |
| CC1 disjoint writes | 100 clients, 100 keys each | every key present |
| CC2 pipelined ordering | 10 clients each pipelining 1000 `INCR` on their own key | replies arrive in issue order and final values are correct |

CC3, the shared-key `INCR` test, is P3.4 above.

### MS8: Benchmark harness (Week 7, Ritk)

| PR | Content |
|---|---|
| P8.1 | `bench/run.sh`: takes `--target rfs|redis`, starts the server with the scenario's persistence flags in a fresh temp dir, runs the warm-up, runs three timed runs of `redis-benchmark --csv`, restarts between runs, writes `bench/results/<date>/<target>/<scenario>-<run>.csv` |
| P8.2 | `bench/sample.sh`: samples the server PID's CPU and `VmHWM` from `/proc` every 100 ms during a run and writes peaks alongside the CSV |
| P8.3 | `bench/spec.sh`: captures CPU model and core count, RAM, kernel, Go version, Redis version into `spec.txt` |
| P8.4 | `bench/cmd/report`: Go program (standard library) that reads the CSVs, computes median and spread per metric, and emits Markdown tables and a CSV for charts |
| P8.5 | `bench/keys-stall.sh`: loads 10^4, 10^5, 10^6 keys with `redis-cli --pipe`, runs `KEYS *` on one connection while a second connection measures `PING` round-trip every millisecond, reports the maximum |
| P8.6 | Scenario definitions as data, so Scenario 3's split invocation and Scenario 5's client sweep and fsync repeat are one config file, not shell branches |

The harness runs against both servers from Week 7 onward at every weekly sync.
Regressions are visible while there is time to fix them.

### MS9: Stretch items (Week 8 and Weeks 9 to 11)

Started only per the cut-line rules.
Listed in build order, which is the reverse of cut order.

**S1 Replication (Weeks 9 to 11, all four).**

| Owner | Work |
|---|---|
| Vighnesh | `REPLICAOF host port` and `REPLICAOF NO ONE`, replica state machine: Connecting, Syncing, Streaming, Disconnected. Handshake: replica sends `SYNC <last_seq>`; primary replies with a bulk string containing a full snapshot generated through the Section 4.6 path, then streams log records in the Section 4.7 format |
| Rajat | Primary side: replica links are clients flagged `replica`; `Propagate` fans each record into every replica's write buffer; backlog is not kept, so a reconnect always performs a full sync, which the report states |
| Ritk | Replica side: apply loop feeds records through the dispatcher in replica mode; write commands from normal clients get `READONLY`; replicas run their own expiry from the absolute timestamps |
| Vivek | Docker Compose topology of one primary and two replicas, test that a write on the primary is visible on both and a write to a replica is rejected, replication flow diagram |

**S2 Sorted sets (Week 8, Ritk).** Pugh skiplist with max level 32 and p = 0.25, `ZADD` (with score update path), `ZSCORE`, `ZRANK`, `ZRANGE` with `WITHSCORES`, snapshot and log support, rank-ordering invariant test against a sorted-slice reference.

**S3 Log compaction (Week 8, Rajat).** As Section 4.8. Test: write, `BGSAVE`, keep writing during compaction, kill, restart, verify everything.

**S4 Transactions (Week 8, Vighnesh).** Per-client queue, `MULTI`, `EXEC`, `DISCARD`, `EXECABORT` on a queued command with wrong arity or unknown name, each queued write gets its own sequence number at `EXEC` time. Commands flagged `NoTx` (`MULTI`, `EXEC`, `DISCARD`, `SUBSCRIBE`, `BGSAVE`) are rejected inside a transaction.

**S5 Pub/Sub (Week 8, Vivek).** Channel to subscriber-set map, subscribe mode on the client rejecting everything but `SUBSCRIBE`, `UNSUBSCRIBE`, `PING`, `QUIT`, `PUBLISH` returning the receiver count, messages written straight into subscriber write buffers, nothing logged. Test with `redis-cli SUBSCRIBE` in one process and `PUBLISH` in another.

### MS10: Evaluation (Week 12, all four, Ritk leads)

1. Run `bench/spec.sh` and commit the output.
2. Build official Redis 7.x from source or install the distribution package; record the exact version.
3. For each scenario in Table 7.1, run both targets with matched persistence flags. Redis is started with `--save "" --appendonly yes --appendfsync <policy> --bind 127.0.0.1 --io-threads 1`.
4. Scenario 5 runs the client sweep at 1, 10, 50 and 200 and the whole sweep again at `always`.
5. Run the `KEYS` stall measurement at all three keyspace sizes on both targets.
6. Generate tables and charts with `bench/cmd/report` and a plotting script in `bench/plot/`.
7. Write the attribution for every gap, testing each candidate from Proposal Section 8 with a targeted experiment where possible: run with `GOGC=off` for one scenario to size the GC share, and count allocations per command with `go test -benchmem` on the codec.

Both servers are pinned with `taskset` to the same core, and the benchmark client to different cores, so the single-core bound is the same for both.

### MS11: Hardening, packaging, documentation (Weeks 13 to 14, all four)

- `make test` green from a clean clone on a machine that has never built the project.
- `deploy/Dockerfile` (multi-stage, static binary, `scratch` base) and `deploy/docker-compose.yml` for the demo topology, verified against the final binary.
- README: build, run, flags, persistence guarantees in one paragraph each, how to run the tests and the benchmark.
- Diagrams finalised from the Graphviz sources: system architecture, persistence flow, request lifecycle, and replication flow if S1 survived.
- Final report: the proposal sections updated to past tense, the evaluation results, the attribution analysis, known limitations, and future work covering cluster mode, `SCAN`, RESP3, automatic failover and a monotonic-clock expiry scheme.
- `docs/VIVA-LEDGER.md` complete, with two names beside every question.
- Two full rehearsals of the Proposal Section 13 script, secondaries answering.

## 6. Definition of Done per Item

| Item | Done when |
|---|---|
| M1 | P1.1 merged, and `redis-cli` interactive, `redis-cli --pipe`, and `redis-benchmark -t ping` all work with no server-side special cases beyond Section 2 items 5 and 6 |
| M2 | P1.3 passes: 100 open connections, goroutine count stays at the fixed baseline of loop plus fsync plus runtime goroutines |
| M3 | P2.1 to P2.4 merged, redis-cli session test green |
| M4 | P3.1 to P3.3 merged, EX1 to EX4 green |
| M5 | P4.1 to P4.4 merged with the property test green |
| M6 | P4.5 and P4.6 merged |
| M7 | P5.1 to P5.4 merged, CR1 and CR4 green |
| M8 | P6.1 to P6.4 merged, CR2 and CR3 green |
| M9 | Every test in MS7 plus P3.4 green in CI from a clean checkout with `make test` |
| M10 | Every table in Table 7.1 filled for both targets with median and spread, stall table filled, attribution written and reviewed by all four |
| M11 | README, four diagrams, Docker Compose verified, final report submitted |
| S1 | Vivek's two-replica test green, diagram drawn, report paragraph on what replication does not provide written |
| S2 | Rank-ordering invariant test green, `ZRANGE` output matches Redis for the same input in a redis-cli session |
| S3 | Compaction test green, file size observed to drop after `BGSAVE` |
| S4 | `EXECABORT` and queued-write sequencing tests green |
| S5 | Cross-process subscribe and publish test green |

## 7. Test Strategy Summary

| Layer | Location | Runs in | Tooling |
|---|---|---|---|
| Unit | `internal/*/..._test.go` | every `go test` | fake clock, table tests, fuzz on the RESP reader |
| Property | `internal/store`, `internal/command` | every `go test` | random operation sequences against slice or map reference models |
| Integration | `test/integration` | `make test`, CI | `ServerProcess` harness, test RESP client, real `redis-cli` for compatibility sessions |
| Crash and expiry | `test/integration` | `make test`, CI | SIGKILL through the harness, hand truncation, debug delay flag |
| Concurrency | `test/integration` | `make test`, CI | 100 goroutine clients in the test process |
| Benchmark | `bench/` | manually in Week 7 onward, Week 12 formally | `redis-benchmark`, `/proc` sampling |

CI runs on every pull request on `ubuntu-latest` with Go 1.22 and `redis-tools` installed.
A red CI blocks merge with no exceptions, per the global engineering rule that flaky or failing tests are never ignored.

## 8. Checkpoints and the Cut Line Procedure

Unchanged from the implementation plan, restated here so this document stands alone:

| Week | Gate | If failed |
|---|---|---|
| 2 | MS0 exit criteria on all four machines, contracts frozen | Extend Phase 0 one week. Do not start tracks. |
| 6 | M1, M3, M4, M5, M7 merged, each presented by its secondary | Vivek takes over snapshot format from Rajat. |
| 8 | Every Must item except M10 and M11 merged, harness runs against both targets | Cut S1. All four on the outstanding item. |
| 11 | S1 demo works or is cut cleanly | Proceed to Phase 3 regardless. |
| 14 | All seven success criteria hold | Ship what exists, document the gap. |

Cutting a stretch item means: the branch stays, the report lists it under future work with a sentence on how far it got, and the command names return `unknown command` so tooling behaves predictably.

## 9. Technical Risk Register

These are the risks in addition to the schedule and team risks already in Proposal Section 11.

| Risk | Signal | Mitigation |
|---|---|---|
| Raw epoll in Go proves harder than expected | No `PONG` from the epoll loop by Week 2 | Week 2 gate extends Phase 0; the design is not swapped silently |
| Copy-on-write during snapshot missed in one handler | CR2 shows a value that changed during `BGSAVE` inside the snapshot | Every collection mutation goes through `Store.Mutable`, enforced by a lint that greps handlers for direct map access, plus P6.2's unit test |
| `everysec` fsync goroutine races with `always` policy switching | Not applicable, policy is fixed at startup | Policy is a startup flag only, never changed at runtime |
| `redis-cli` or `redis-benchmark` version sends a command we did not anticipate | Client hangs or errors on connect | MS0 exit criteria pin the versions and test the connect handshake; CI installs the same version |
| GC pauses dominate tail latency and hide other attributions | p99.9 far above p99 | Evaluation step 7 runs a `GOGC=off` control on one scenario to size the effect |
| Active sweep starves the loop on a huge expiring set | `PING` latency rises with `expires` count | Sweep is bounded at 1 ms per tick; the bound is measured in the `KEYS` stall setup |
| `KEYS *` on 10^6 keys allocates a large reply and stalls longer than the scan itself | Stall much larger than map iteration time | Report separates iteration time from serialisation time using INFO counters added for the measurement |

## 10. Document Set

| File | Owner | Updated |
|---|---|---|
| `docs/PROPOSAL.md` | all | frozen after synopsis submission |
| `docs/IMPLEMENTATION-PLAN.md` | Vighnesh | at each checkpoint |
| `docs/MASTER-PLAN.md` | Vighnesh | when a contract or milestone changes, with all four approving |
| `docs/CONTRACTS.md` | all | Week 2 freeze, then only by unanimous PR |
| `docs/DECISIONS.md` | whoever makes the decision | continuously |
| `docs/VIVA-LEDGER.md` | secondaries | after every weekly sync |
| `docs/diagrams/*.dot` | Vivek | Week 2 drafts, Week 14 final |
| `docs/REPORT.md` | all | Weeks 12 to 14 |
