# RedForge

A Redis-compatible server written from scratch in Go, using only the standard library.

`redis-cli` and `redis-benchmark` talk to it without knowing the difference.
There is no `net` package, no goroutine per connection and no dependency: one thread, one `epoll` loop, one keyspace.
Writes go to a checksummed append-only log, `BGSAVE` takes snapshots without `fork`, and the server survives `kill -9` with its data and its TTLs intact.

B.Tech final-year project.
Module path `github.com/SVIGHNESH/RedForge`, Go 1.22, **Linux only** (see [Linux only](#linux-only)).

## Status

Every correctness item of the proposal, M1 to M9, is merged: the protocol, the event loop, strings, expiry, lists, hashes, sets, the append-only log, snapshots with crash recovery, and the test suite that proves them.

| Done | Left |
|---|---|
| RESP2 codec, pipelining, inline commands, Redis-exact protocol errors | Benchmark runner and evaluation against official Redis (T8.01, T8.04 to T8.06, T10.x) |
| Single-threaded `epoll` loop, 100+ connections on one thread | Stretch: log compaction, transactions, sorted sets, Pub/Sub, replication (T9.x) |
| Strings, keyspace, expiry (passive and active), lists, hashes, sets | README, final diagrams and report (T11.02 to T11.04) |
| Append-only log with three fsync policies, torn-record truncation | |
| `BGSAVE` by clone plus copy-on-write, atomic rename, recovery | |
| Crash, expiry, concurrency and property tests, CI with zero skips | |

The live picture, issue by issue, is the [requirements traceability matrix](docs/RTM.md): what each issue serves, what blocks it, and whose work it waits on.

## Usage

### Build and run

```sh
make build                                   # -> bin/redis-from-scratch
./bin/redis-from-scratch                     # 127.0.0.1:6380, data in ./data
./bin/redis-from-scratch --port 6390         # somewhere else
./bin/redis-from-scratch --dir /var/lib/rfs  # keep the log and snapshot elsewhere
./bin/redis-from-scratch --appendfsync always
./bin/redis-from-scratch --version           # 0.1.0-rfs
```

| Flag | Default | Meaning |
|---|---|---|
| `--port` | `6380` | Port to listen on. Not 6379, so it can run beside a real Redis. |
| `--bind` | `127.0.0.1` | IPv4 address to bind. |
| `--dir` | `./data` | Persistence directory, holding `appendonly.aof` and `dump.snap`. |
| `--appendfsync` | `everysec` | When the log is fsynced: `always` (before every reply), `everysec`, or `no` (the kernel decides). |
| `--debug-bgsave-delay-ms` | `0` | Test hook that slows the snapshot rename so the kill-during-`BGSAVE` test is deterministic. Never used in the evaluation. |
| `--version` | | Print the version and exit. |

Stop the server with Ctrl-C.
`--replicaof` arrives with the replication stretch item (T9.06).

### Linux only

The server is a raw `epoll` loop, so it builds and runs on Linux and nowhere else.
`GOOS=windows go build ./...` fails on `undefined: syscall.EpollCreate1`, and macOS fails the same way, because `epoll` is a Linux kernel interface with no equivalent exposed under those names.
This is a constraint of the project, not a gap: the synopsis rules out a portability layer, so there is deliberately no `net`-package fallback.

On Windows, use WSL2, a Linux VM, or Docker (see [Docker](#docker)).
`internal/resp` and `internal/command` are pure Go and build anywhere; only the server does not.

### Talk to it with redis-cli

```sh
$ redis-cli -p 6380 SET name Vighnesh
OK
$ redis-cli -p 6380 GET name
Vighnesh
$ redis-cli -p 6380 EXPIRE name 100
(integer) 1
$ redis-cli -p 6380 TTL name
(integer) 100
$ redis-cli -p 6380 LPUSH users Vighnesh Rajat
(integer) 2
$ redis-cli -p 6380 LRANGE users 0 -1
1) "Rajat"
2) "Vighnesh"
$ redis-cli -p 6380 HSET user:1 name Ritik role dev
(integer) 2
$ redis-cli -p 6380 SADD a x y z
(integer) 3
$ redis-cli -p 6380 SADD b y z w
(integer) 3
$ redis-cli -p 6380 SINTER a b
1) "y"
2) "z"
$ redis-cli -p 6380 GET users
(error) WRONGTYPE Operation against a key holding the wrong kind of value
```

Interactive mode works, and `quit` exits cleanly.
Anything outside the command set is refused in Redis's own wording:

```sh
$ redis-cli -p 6380 HELLO 3
(error) ERR unknown command 'HELLO', with args beginning with: '3'
$ redis-cli -p 6380 ECHO
(error) ERR wrong number of arguments for 'echo' command
```

### Persistence and crash recovery

Every write is appended to `appendonly.aof` as a record carrying a sequence number, a length and a CRC32.
`BGSAVE` writes `dump.snap` in the background and records the highest sequence number it contains, so on restart the snapshot is loaded and only the log records after it are replayed.

```sh
$ redis-cli -p 6380 SET session abc
$ redis-cli -p 6380 EXPIRE session 60
$ redis-cli -p 6380 BGSAVE
Background saving started
$ redis-cli -p 6380 SET k v            # after the snapshot, so only in the log
$ kill -9 <server pid>; sleep 2; ./bin/redis-from-scratch
recovery: loaded 1 keys from dump.snap (max_seq 2)
recovery: replayed 1 log records after seq 2 (skipped 2 already in the snapshot, last seq 3)
$ redis-cli -p 6380 GET k
"v"
$ redis-cli -p 6380 TTL session        # aged by the time the server was down
(integer) 57
```

A record torn by a crash ends replay: the file is truncated where that record began, and `INFO persistence` reports `aof_truncated_records:1` and the offset.
A snapshot killed half-way leaves the previous `dump.snap` untouched; the orphaned `dump.snap.tmp-<pid>` is deleted at the next start.

### Talk to it with nc

Inline commands work, so the server is reachable from `nc` and `telnet`.
Two commands in one write get two replies, which is pipelining:

```sh
$ printf 'PING\r\nPING\r\n' | nc -q1 localhost 6380
+PONG
+PONG
```

A malformed frame earns a protocol error and a closed connection, rather than a desynchronised stream:

```sh
$ printf '*1\r\n+x\r\n' | nc -q1 localhost 6380
-ERR Protocol error: expected '$', got '+'
```

### INFO

`redis-cli INFO` and `redis-cli --stat` work.
The sections are `Server`, `Clients`, `Stats`, `Persistence`, `Replication` and `Keyspace`, in Redis's format (MASTER-PLAN Section 4.10).
`goroutines` in the `Server` section is a project field, not a Redis one: it lets the 100-connection test prove that open connections add no goroutines.

### Docker

```sh
make docker    # build the image from deploy/Dockerfile
make demo      # start deploy/docker-compose.yml in the background
docker compose -f deploy/docker-compose.yml run --rm cli PING
```

The image is a static binary on `scratch`, built with `golang:1.22` to match `go.mod` and CI, and keeps its data in a volume mounted at `/data`.

### Benchmark tools

```sh
bench/spec.sh > spec.txt           # CPU, cores, RAM, kernel, governor, Go and Redis versions
bench/sample.sh <pid> <out.csv>    # CPU and memory of a process every 100 ms, as CSV
bench/peaks.sh <out.csv>           # peak CPU and memory from that file
```

The runner that drives `redis-benchmark` against both servers (T8.01) is not written yet, because it needs official Redis installed as the baseline.

## Design

Three decisions carry the whole project; [DECISIONS.md](docs/DECISIONS.md) records each with the alternatives rejected.

**One thread, raw epoll.**
The loop calls `syscall.EpollCreate1`, `Accept4` and `EpollWait` directly and runs under `runtime.LockOSThread`.
Because a single thread owns the keyspace, no command needs a lock and no read can observe a half-applied write.
Concurrency lives in the socket multiplexing, not in the data.

**Timers inside the loop.**
The `EpollWait` timeout is a 100 ms tick that drives the active expiry sweep, the `everysec` fsync request and the `BGSAVE` completion check.
No `time.Ticker` goroutine ever touches state.
Besides the loop there are at most three goroutines (fsync, snapshot writer and, with S3, compaction), none of which touches the live keyspace; each signals the loop through an `eventfd` in the epoll set.

**Absolute expiry timestamps.**
No relative TTL is persisted or replicated anywhere; `EXPIRE` is rewritten to `PEXPIREAT k <abs ms>` before it reaches the log.
A key that expired while the server was down stays expired after recovery, and one that did not comes back with its TTL aged.

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

`QUIT` closes the connection, and `PEXPIREAT` is accepted because it is the form `EXPIRE` takes in the log.
The stretch commands (`ZADD`, `MULTI`, `SUBSCRIBE`, `REPLICAOF` and their families) are not implemented yet and return `unknown command`.

## Layout

```
cmd/redis-from-scratch   flags, recovery, serve
internal/resp            RESP2 reader and writer
internal/server          epoll loop, connections, buffers, eventfd, INFO
internal/command         command table, dispatcher, handlers, replay
internal/store           keyspace, value model, expiry, copy-on-write snapshot
internal/storetest       FakeClock for deterministic expiry tests
internal/aof             log record codec, writer and fsync policies, replay
internal/snapshot        snapshot format, writer, loader
internal/recovery        startup sequence: snapshot, log tail, expired keys
internal/testutil        ServerProcess harness and RESP client for tests
test/integration         redis-cli, crash, expiry, concurrency, property tests
bench/                   machine spec and CPU/memory sampling scripts
deploy/                  Dockerfile and docker-compose.yml
scripts/tasks/           GitHub issue sync and the RTM generator
```

## Development

```sh
make build        # bin/redis-from-scratch
make test         # build, then go test -race over unit and integration tests
make test-short   # the same, skipping the tests that sleep for seconds
make lint         # gofmt -l . then go vet ./...
make diagrams     # render docs/diagrams/*.dot to PNG (needs graphviz)
make rtm          # regenerate docs/RTM.md from GitHub (needs gh)
make docker       # build the container image
```

Integration tests run the real binary through `internal/testutil` and need `redis-tools` installed for `redis-cli` and `redis-benchmark`.
CI fails if any test is skipped.
A single test: `go test ./internal/store -run TestName -v`.

## Documentation

- [PROPOSAL.md](docs/PROPOSAL.md) - scope, and the invariants the tests must prove.
- [IMPLEMENTATION-PLAN.md](docs/IMPLEMENTATION-PLAN.md) - phases and checkpoints.
- [MASTER-PLAN.md](docs/MASTER-PLAN.md) - layout, on-disk formats, milestones. Read this before writing code.
- [CONTRACTS.md](docs/CONTRACTS.md) - the interfaces frozen at the end of Week 2, and what was added since.
- [DECISIONS.md](docs/DECISIONS.md) - why each design choice was made and what was rejected.
- [RTM.md](docs/RTM.md) - requirements traceability: every issue, its requirement, its blockers and its status.
- [VIVA-LEDGER.md](docs/VIVA-LEDGER.md) - viva questions with answers and evidence.
- [docs/diagrams/](docs/diagrams/) - architecture and request lifecycle, as Graphviz sources and PNGs.
- [docs/tasks/](docs/tasks/README.md) - the board: one self-contained task per file, with dependencies and the critical path.

## Constraints

`go.mod` must never gain a `require` block.
No command may be added outside the list above.
No relative TTL may be persisted.
Every pull request carries the checklist in MASTER-PLAN Section 1.3 and needs two approvals; red CI blocks the merge.
