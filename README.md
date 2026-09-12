# RedForge

A Redis-compatible server written from scratch in Go, using only the standard library.

`redis-cli` and `redis-benchmark` talk to it without knowing the difference.
There is no `net` package, no goroutine per connection and no dependency: one thread, one `epoll` loop, one keyspace.

B.Tech final-year project.
Module path `github.com/SVIGHNESH/RedForge`, Go 1.22, Linux only.

## Status

Phase 0 of [the task board](docs/tasks/README.md) is merged: the RESP2 codec, the event loop and the dispatcher.

| Working today | Next |
|---|---|
| `PING`, `ECHO`, `QUIT` over RESP2 and inline | strings and the keyspace (T0.07, T2.x) |
| Pipelining, partial reads, protocol errors | expiry (T3.x), the append-only log (T5.x) |
| 100+ connections on one thread | snapshots and recovery (T6.x) |

## Try it

```sh
make build
./bin/redis-from-scratch --port 6380
```

```sh
$ redis-cli -p 6380 PING
PONG
$ redis-cli -p 6380 ECHO hi
hi
$ redis-cli -p 6380 FOO a b
ERR unknown command 'FOO', with args beginning with: 'a' 'b'
$ redis-benchmark -p 6380 -t ping -n 100000 -P 16 -q
PING_MBULK: 714285.69 requests per second, p50=1.103 msec
```

The default port is **6380**, not 6379, so the server can run next to a real Redis for comparison.

## Design

Three decisions carry the whole project.

**One thread, raw epoll.**
The loop calls `syscall.EpollCreate1`, `Accept4` and `EpollWait` directly and runs under `runtime.LockOSThread`.
Because a single thread owns the keyspace, no command needs a lock and no read can observe a half-applied write.
Concurrency lives in the socket multiplexing, not in the data.

**Timers inside the loop.**
The `EpollWait` timeout is a 100 ms tick that will drive active expiry, the `everysec` fsync request and the BGSAVE completion check.
No `time.Ticker` goroutine ever touches state.

**Absolute expiry timestamps.**
No relative TTL is persisted or replicated anywhere; `EXPIRE` is rewritten to `PEXPIREAT k <abs ms>` before it reaches the log.
A key that expired while the server was down stays expired after recovery.

Exactly three background goroutines are planned (fsync ticker, snapshot writer, compaction copier), none of which touches the live keyspace; each signals completion back through an `eventfd` in the epoll set.

## Commands

The command set is closed.
Anything outside it, `HELLO` included, returns `-ERR unknown command 'x'`, and error strings match Redis's wording verbatim so client tools behave identically.

```
PING ECHO INFO COMMAND BGSAVE
SET GET DEL EXISTS INCR KEYS
EXPIRE TTL PERSIST
LPUSH RPUSH LPOP RPOP LLEN LRANGE
HSET HGET HDEL HLEN HGETALL
SADD SREM SISMEMBER SMEMBERS SINTER
```

## Layout

```
cmd/redis-from-scratch   flags, recovery, serve
internal/resp            RESP2 reader and writer
internal/server          epoll loop, connections, buffers
internal/command         command table, dispatcher, handlers
internal/store           keyspace, value model, expiry, clock
internal/aof             log records, fsync policy, replay
internal/snapshot        snapshot format, writer, loader
internal/recovery        startup sequence
test/integration         redis-cli, crash, expiry, concurrency tests
bench/                   benchmark harness and results
```

## Development

```sh
make build   # bin/redis-from-scratch
make test    # go test ./..., unit and integration
make lint    # gofmt -l . then go vet ./...
make bench   # see bench/
```

Integration tests need `redis-tools` installed for `redis-cli` and `redis-benchmark`.
A single package: `go test ./internal/store -run TestName -v`.

Server flags: `--port 6380`, `--bind 127.0.0.1`.
The persistence and replication flags land with their tasks.

## Documentation

- [PROPOSAL.md](docs/PROPOSAL.md) - scope, and the invariants the tests must prove.
- [IMPLEMENTATION-PLAN.md](docs/IMPLEMENTATION-PLAN.md) - phases and checkpoints.
- [MASTER-PLAN.md](docs/MASTER-PLAN.md) - layout, frozen contracts, on-disk formats. Read this before writing code.
- [docs/tasks/](docs/tasks/README.md) - the board: one self-contained task per file, with dependencies and the critical path.

## Constraints

`go.mod` must never gain a `require` block.
No command may be added outside the list above.
No relative TTL may be persisted.
Every pull request carries the checklist in MASTER-PLAN Section 1.3 and needs two approvals; red CI blocks the merge.
