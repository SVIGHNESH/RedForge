# Technical Decisions

One entry per decision, written while the reasons are fresh.
Each entry answers "why not the alternative" and is short enough to read aloud in under a minute.
The source for every entry is [MASTER-PLAN.md](MASTER-PLAN.md) Sections 3 and 4; the section is named in each heading.
This file is also the source for viva answers on "why did you do it this way".

## D1. Event loop over raw `epoll` through `syscall` (MASTER-PLAN 3.1)

**Context:** The synopsis promises a single-threaded event loop over non-blocking sockets, with no goroutine per connection.
Go's `net` package hides its poller and hands each accepted connection to whoever calls `Accept`, which pushes every design toward one goroutine per connection.

**Decision:** The server calls `Socket`, `Bind`, `Listen`, `Accept4`, `EpollCreate1`, `EpollCtl` and `EpollWait` from the `syscall` package directly, and the loop goroutine calls `runtime.LockOSThread` so it is one OS thread.
`syscall` is part of the standard library, so the standard-library-only rule still holds.
Linux is the only supported operating system, so there is no portability layer.

**Rejected:** The `net` package with a goroutine per connection feeding one executor goroutine over a channel.
It would keep commands atomic, but it breaks the M2 wording and adds a channel hop to every request, which would blur the benchmark comparison with Redis.

**Consequence:** We own partial reads, partial writes, `EPOLLOUT` registration and pipelining ourselves, in `internal/server`.
The 100-connection test can prove that connection count does not drive goroutine count.

## D2. Three background goroutines that never touch the keyspace (MASTER-PLAN 3.2)

**Context:** Some work cannot run on the loop without stalling every client: `fsync` under `everysec`, writing a snapshot, and copying the log during compaction.

**Decision:** Exactly three goroutines exist besides the loop: the fsync ticker, the snapshot writer and the compaction copier.
None of them reads or writes the live keyspace.
The fsync goroutine only calls `Fsync` on the log descriptor, which is safe alongside `Write` on the same descriptor.
The snapshot writer reads only a cloned map and values that no handler will mutate again (D6).
The copier reads the old log file and writes a new one.
Completion is signalled back through an `eventfd` registered in epoll, so the loop learns about it like any other readiness event.
Replica links (S1) are ordinary epoll descriptors on the loop, not goroutines.

**Rejected:** A mutex around the keyspace so background work can read it directly.
Every command would pay for the lock, and a long snapshot would hold it and stall clients, which is exactly what `BGSAVE` exists to avoid.

**Consequence:** There is no lock anywhere on the request path.
The ownership argument is simple enough to state in one sentence: only the loop thread touches live state.

## D3. Timers live inside the loop (MASTER-PLAN 3.3)

**Context:** Three things must happen periodically: the active expiry sample, the `everysec` fsync request, and the check for `BGSAVE` completion.

**Decision:** `EpollWait` is called with a 100 ms timeout, and the tick runs after every wake-up on the loop thread.
There is no `time.Ticker` goroutine that touches state.

**Rejected:** A `time.Ticker` goroutine per timer.
It would have to touch the keyspace (expiry) or loop state (bgsave status) from another goroutine, which reintroduces the locks D2 removes.

**Consequence:** Timer work is serialised with commands for free.
A long command delays the tick, which is the same trade Redis makes, and the active sweep is bounded at 1 ms per tick so the tick cannot starve clients.

## D4. The clock is injected (MASTER-PLAN 3.4)

**Context:** Expiry correctness depends on time, and tests that sleep through real TTLs are slow and flaky.

**Decision:** Every time read goes through a `Clock` interface with one method, `NowMs() int64`.
Production uses the wall clock, as the synopsis states.
Unit tests use `storetest.FakeClock`, which only moves when the test moves it.
The dispatcher reads the clock once per command into `Ctx.Now`, so every step of one command sees the same instant.

**Rejected:** Calling `time.Now()` directly.
Expiry tests would need real sleeps and tolerances, and a command could see the time change halfway through.

**Consequence:** Expiry unit tests are deterministic and run in microseconds.
Integration tests still use the real clock, with the tolerance stated in each test.

## D5. Expiry deletions are not propagated (MASTER-PLAN 4.4)

**Context:** The active sweep and passive lookups delete expired keys.
Every other state change is written to the log and sent to replicas through `Propagate`.

**Decision:** Deletions made by expiry, active or passive, do not call `Propagate` and do not consume a sequence number.
Every key carries an absolute `expire_at` in Unix milliseconds, and `EXPIRE` is logged as `PEXPIREAT k <absolute ms>`.
Replay and replicas drop expired keys from that timestamp on their own.

**Rejected:** Logging a `DEL` for every expired key, as Redis does for replicas.
It adds log records without adding information, because the timestamp already decides the outcome, and it makes the sweep's cost show up as log traffic.

**Consequence:** No relative TTL is ever persisted or replicated.
A key that expired while the server was down is dropped at startup and counted in `expired_keys`.
A replica's view of an expired key can differ from the primary's only by clock skew between the two machines, which the report states.

## D6. `BGSAVE` by shallow clone plus copy-on-write, without `fork` (MASTER-PLAN 4.6)

**Context:** Redis snapshots by forking and letting the kernel copy pages on write.
A Go process cannot fork safely, because the child would inherit only the forking thread of a multi-threaded runtime.

**Decision:** On the loop thread, `BGSAVE` records the boundary `seq` and copies the top-level `map[string]*Entry` into a new map.
Entries are shared, not copied.
While the snapshot is active, `Store.Mutable` clones a collection value before returning it and stores the clone in the live map, so the writer goroutine only ever sees values no handler will mutate.
`Put` and `Delete` change only the live map, which the writer never reads.
The writer serialises to `dump.snap.tmp-<pid>`, fsyncs it, renames it over `dump.snap`, fsyncs the directory and signals the `eventfd`.

**Rejected:** Serialising the whole keyspace on the loop thread, which stalls every client for the full write.
Also rejected: a deep copy of every value up front, which doubles memory and still costs O(data) on the loop.

**Consequence:** The only stop-the-world cost is the O(keys) map copy, which is measured and reported as `last_bgsave_clone_ms`.
Every collection mutation must go through `Store.Mutable`, never direct map access, or a mutation could leak into the snapshot.
A crash before the rename leaves the old `dump.snap` intact and an orphan temp file, which startup deletes.

## D7. Log compaction is triggered by `BGSAVE`, not by a command (MASTER-PLAN 2 item 7 and 4.8)

**Context:** Stretch item S3 asks for log compaction, but the command matrix has no compaction command such as `BGREWRITEAOF`, and the matrix is closed.

**Decision:** After every successful `BGSAVE` with boundary N, the copier goroutine writes `appendonly.aof.new` holding only records with `seq > N`.
When it signals completion, the loop appends any records written since, fsyncs, renames the new file over `appendonly.aof`, and continues appending to the new descriptor.

**Rejected:** Adding `BGREWRITEAOF`, which would put a command outside the frozen matrix.
Also rejected: rewriting the log from the keyspace as Redis does, which duplicates the snapshot work the `BGSAVE` just did.

**Consequence:** The log never grows past one snapshot interval of writes.
The snapshot and the log can never disagree, because replay applies only records with `seq` above the snapshot's `max_seq`.
