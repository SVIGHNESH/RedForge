# T0.04 Epoll listener and connection registry

**Item:** M2 | **Milestone:** MS0 | **Week:** 1 to 2
**Suggested owner:** pair on this one, it is the riskiest Phase 0 task | **Reviewer:** Vighnesh
**Effort:** 5 to 6 hours
**Depends on:** T0.01
**Unblocks:** T0.05

## Goal

`internal/server` accepts TCP connections with raw `epoll` from the `syscall` package on one locked OS thread, and reads bytes from them without any goroutine per connection.
No parsing yet. A connection that sends bytes gets them echoed back, purely to prove the loop works.

## Context you need

- MASTER-PLAN Section 3.1 explains why `net.Listen` is not used.
- `syscall` is standard library. Linux only. Use `syscall.EpollCreate1`, `EpollCtl`, `EpollWait`, `Socket`, `SetsockoptInt` (`SO_REUSEADDR`), `Bind`, `Listen`, `Accept4` with `SOCK_NONBLOCK|SOCK_CLOEXEC`, `Read`, `Write`, `Close`.
- Level-triggered epoll (no `EPOLLET`) keeps the logic simple: if data is left unread, the next `EpollWait` returns the fd again.
- The loop goroutine calls `runtime.LockOSThread()` at start.

## Steps

1. `type Server struct { epfd, lfd int; clients map[int]*Client; ... }` and `type Client struct { fd int; rbuf, wbuf []byte; closing bool }`.
2. `Listen(bind string, port int)` creates the socket, sets non-blocking and `SO_REUSEADDR`, binds, listens with backlog 511, registers the listener fd for `EPOLLIN`.
3. `Serve()` loops: `EpollWait(epfd, events, timeoutMs)`. For the listener fd, accept in a loop until `EAGAIN`, register each new fd for `EPOLLIN`, create a `Client`. For a client fd with `EPOLLIN`, read into a 16 KB scratch buffer until `EAGAIN`, append to `rbuf`. On `EOF` or error, close and delete.
4. For now, after reading, move `rbuf` into `wbuf` and try to write; if the write is short, keep the remainder and register `EPOLLOUT`; on `EPOLLOUT` continue writing and deregister when drained.
5. Handle `EINTR` on `EpollWait` by retrying.
6. `Close()` shuts the listener and every client.
7. Add `connected_clients` and `total_connections_received` counters on `Server` for INFO later.
8. Test with `nc localhost 6380`, and with `for i in $(seq 100); do nc -z localhost 6380 & done` to confirm 100 connections register.

## Acceptance

- [ ] Echo works over `nc`.
- [ ] 100 concurrent connections are accepted and `runtime.NumGoroutine()` printed from a debug log stays constant.
- [ ] Closing a client from the other side removes it from `clients` with no leaked fd (`ls /proc/<pid>/fd | wc -l` returns to baseline).
- [ ] `go vet` clean.
