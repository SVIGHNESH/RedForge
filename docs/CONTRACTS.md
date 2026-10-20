# Frozen Contracts

> **Frozen at end of Week 2. Changes require a PR approved by all four members.**

This file is MASTER-PLAN Section 4, checked line by line against the merged code.
Where the code and the plan differed, the difference is listed in "Drift resolved at the freeze" at the end, and the Go snippets below show the code as it is.
Section numbers are kept from MASTER-PLAN so references in task files still resolve.
`internal/store/contract_test.go` and `internal/command/contract_test.go` use every method and field shown here, so a signature change fails to compile.

## 4.1 Repository layout

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

## 4.2 Value model

```go
// internal/store/types.go
type Type uint8

const (
	TString Type = iota + 1
	TList
	THash
	TSet
	TZSet
)

type Value interface {
	Clone() Value
}

type Entry struct {
	Type     Type
	Val      Value // *StringVal, *ListVal, *HashVal, *SetVal, *ZSetVal
	ExpireAt int64 // absolute Unix ms, 0 = no expiry
}

type StringVal struct{ B []byte }
type HashVal struct{ M map[string][]byte }
type SetVal struct{ M map[string]struct{} }
type ListVal struct{ /* ring-buffered deque, T4.01 */ }
type ZSetVal struct{ /* skiplist plus score map, T9.01 */ }

// internal/store/clock.go
type Clock interface {
	NowMs() int64
}
type RealClock struct{}
```

`StringVal` wraps `[]byte` and is never mutated in place; `INCR` and `SET` replace it.
`ListVal` is a deque over a ring-buffered slice giving O(1) amortised push and pop at both ends.
`HashVal` and `SetVal` wrap Go maps.
`ZSetVal` (S2) is a skiplist plus a `map[string]float64` for O(1) score lookup.
Every `Value` implements `Clone() Value`, which the copy-on-write path in Section 4.6 needs.

## 4.3 Store interface

```go
// internal/store/types.go
type Store interface {
	// Lookup applies passive expiry: an expired key is deleted and reported absent.
	Lookup(key string) (*Entry, bool)
	// Mutable returns the entry for in-place mutation, cloning the value first
	// if a snapshot is in progress. Also applies passive expiry.
	Mutable(key string) (*Entry, bool)
	Put(key string, e *Entry)
	Delete(key string) bool
	Exists(key string) bool
	Keys(pattern string) []string // glob per Redis: * ? [abc] \x
	SetExpireAt(key string, atMs int64) bool
	Persist(key string) bool             // true if a TTL was removed
	TTLms(key string) (int64, TTLStatus) // NoKey, NoExpire, HasExpire
	Len() int
	ExpiringLen() int
	SweepExpired(sampleSize int, budget time.Duration) int // keys deleted
	Snapshot() SnapshotView                                // shallow clone for the writer, see 4.6
	ForEach(fn func(key string, e *Entry) bool)
}

type TTLStatus uint8 // NoKey, NoExpire, HasExpire

type SnapshotView interface {
	ForEach(fn func(key string, e *Entry) bool)
	Release()
}

func New(clock Clock) Store // map-backed; nil clock means RealClock
```

`Keys` matches the Redis glob rules so `redis-cli --scan`-free tooling and the tests behave identically on both servers.

## 4.4 Command handler signature and the propagate hook

```go
// internal/command/command.go
type Handler func(ctx *Ctx, args [][]byte) resp.Reply

type Ctx struct {
	Client *server.Client
	Store  store.Store
	Srv    *server.Server // stats, INFO fields, bgsave state, seq, Appenders
	Now    int64          // ms, read once per command from the Clock
	// unexported: the pending canonical form set by Propagate
}

// Propagate records the canonical write form of this command. Calling it
// twice in one command panics. The dispatcher appends it with a fresh
// sequence number after the handler returns a non-error reply.
func (ctx *Ctx) Propagate(args ...[]byte)

type Flags uint8 // Write | ReadOnly | Admin | PubSub | NoTx

type Command struct {
	Name    string
	Arity   int // counts the name; negative means "at least"
	Flags   Flags
	Handler Handler
}

func Dispatch(ctx *Ctx, args [][]byte) resp.Reply
func Install(s *server.Server, st store.Store, clock store.Clock)

// internal/server/server.go
// Appender consumes one propagated write: the AOF writer (T5.02) and the
// replication fan-out (T9.07). It lives in server to avoid an import cycle.
type Appender interface {
	Append(seq uint64, args [][]byte) error
}

func (s *Server) NextSeq() uint64     // dispatcher only, loop thread
func (s *Server) CurrentSeq() uint64  // INFO and the snapshot boundary
func (s *Server) SetSeq(n uint64)     // recovery and replicas
```

Rules:

- Read commands never call `Propagate` and never consume a sequence number.
- A handler that changes state calls `Propagate` exactly once with the canonical form. `EXPIRE k 10` propagates `PEXPIREAT k <absolute ms>`. `INCR k` propagates `INCR k`, since replay re-derives the same value. `SET k v` propagates itself.
- A handler that fails, or that turns out to be a no-op such as `DEL` on a missing key, does not propagate.
- Deletions performed by the expiry sweep are not propagated. Replay and replicas drop expired keys from the absolute timestamp, so logging the deletion would add records without adding information. This is recorded in `DECISIONS.md`.

## 4.5 Sequence number contract

- `seq` is a `uint64`, starts at 0 on a fresh directory, and is incremented by exactly one per propagated write.
- It is assigned on the loop thread inside the dispatcher, never inside a handler.
- Every log record carries its `seq`. The snapshot carries `max_seq`, the highest `seq` whose effect it contains.
- On recovery the counter resumes from `max(snapshot.max_seq, last valid log record seq)`.
- Replay applies only records with `seq > snapshot.max_seq`, which is what makes the two files unable to disagree.
- Under S1 a replica applies records with the primary's `seq` and never assigns its own.

## 4.6 `BGSAVE` without fork

1. The handler rejects a second `BGSAVE` while one is running with `-ERR Background save already in progress`.
2. On the loop thread: record `boundary = seq`, call `Store.Snapshot()`, which copies the top-level `map[string]*Entry` into a new map and sets `snapshotActive = true`. Entries are shared, not copied. The duration of this copy is recorded in INFO as `last_bgsave_clone_ms`.
3. The snapshot goroutine serialises the cloned map to `dump.snap.tmp-<pid>`, `fsync`s the file, renames it over `dump.snap`, `fsync`s the directory, and writes to the `eventfd`.
4. While `snapshotActive`, `Store.Mutable` clones a collection value before returning it and stores the clone in the live map. The snapshot goroutine therefore only ever sees values that no handler will mutate. `Put` and `Delete` act on the live map only, which the goroutine never reads.
5. On the `eventfd` readiness the loop clears `snapshotActive`, drops its reference to the clone, and updates `last_bgsave_status`, `last_bgsave_seq` and `last_bgsave_time`.

A crash at any point before the rename leaves the previous `dump.snap` intact and a `dump.snap.tmp-*` orphan, which startup deletes.
This is exactly Proposal Section 6.4 and the sequence in Fig. 5.5.

## 4.7 Log record format

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

## 4.8 Snapshot format

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

## 4.9 Startup recovery sequence

```
delete dump.snap.tmp-*
if dump.snap exists and verifies: load it, seq = max_seq
open appendonly.aof (create with header if missing)
replay records with seq > max_seq; stop and truncate at the first bad record
drop every key whose expire_at <= now; count them as expired_keys
seq = max(seq, last replayed seq)
start listening
```

## 4.10 INFO fields

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

## 4.11 Error strings

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

## Drift resolved at the freeze

Each line below is a place where the merged code and MASTER-PLAN Section 4 disagreed, and what this freeze settles on.

- `Ctx` had no `Store` field and `main` built no store. Fixed in code by PR #83 so the contract holds as written.
- `Install` takes the `Clock` instead of a `now func() int64`, so `Ctx.Now` and passive expiry read the same clock (PR #83).
- The `Appender` interface and the `NextSeq`, `CurrentSeq`, `SetSeq` methods were added by T0.08 and were not in MASTER-PLAN. They are frozen here as the only way to reach the sequence counter.
- `TTLms` and `SweepExpired` have unnamed results in code. The meaning (`remainingMs, status` and `deleted`) is unchanged and is stated in the comments.
- `SnapshotView` and `TTLStatus` were referenced but not defined in MASTER-PLAN. They are frozen as defined in `internal/store/types.go`.

## Additions outside the frozen contract

These came in with Phase 1 PRs.
They extend the code without changing any signature above, so they are not frozen and may change without a unanimous PR.

- `store.Counters` (T3.01, #93): an optional interface with `ExpiredKeys()` and `SweepDeleted()` for INFO. It sits beside `Store` instead of adding methods to it.
- `Server.OnTick(fn func())` (T3.02, #94) replaces the single `Server.Tick` field with a list of hooks run every 100 ms on the loop thread. The AOF `everysec` request (T5.02) and the `BGSAVE` completion check (T6.03) register through it.
