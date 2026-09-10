# T0.05 Read and write buffers with incremental parsing and pipelining

**Item:** M1, M2 | **Milestone:** MS0 | **Week:** 2
**Suggested owner:** whoever did T0.04 | **Reviewer:** Vivek
**Effort:** 3 to 4 hours
**Depends on:** T0.02, T0.04
**Unblocks:** T0.06

## Goal

The loop parses as many complete commands as the read buffer holds, executes each in order, queues each reply into the write buffer, and flushes once.
Partial frames wait for the next read. This is pipelining, and it is a Phase 0 property because Scenario 5 and the M9 tests depend on it.

## Context you need

- `resp.ParseCommand(buf) (args, n, err)` returns `ErrIncomplete` when a frame is not fully present.
- Execution for now is a stub `func(args [][]byte) resp.Reply` that returns `+OK`. T0.06 replaces it with the dispatcher.

## Steps

1. After appending new bytes to `c.rbuf`, loop: call `ParseCommand`; on `ErrIncomplete` break; on `ErrProtocol` write `-ERR Protocol error: ...\r\n`, mark `closing`, break; otherwise execute, `reply.AppendTo(c.wbuf)`, advance `c.rbuf = c.rbuf[n:]`.
2. Compact `rbuf` when the consumed prefix exceeds half its capacity, so a long-lived client does not grow memory.
3. After the loop, attempt one `Write` of `wbuf`. On short write register `EPOLLOUT` and keep the tail. On `EAGAIN` same. Do not read from a client whose `wbuf` exceeds 64 MB; that is the output-buffer limit, close it instead.
4. A client marked `closing` is closed after its `wbuf` drains.
5. Guard: a single frame larger than 512 MB is a protocol error from T0.02, so `rbuf` cannot grow unbounded.
6. Tests in `internal/server`: feed a connection two commands in one write and assert two replies; feed one command split across three writes and assert one reply after the third; feed 1000 pipelined `PING`s and assert 1000 `+PONG` in order.

## Acceptance

- [ ] `printf 'PING\r\nPING\r\n' | nc localhost 6380` prints `+PONG` twice.
- [ ] The three server tests pass.
- [ ] Sending garbage `\x00\x01` gets a `-ERR Protocol error` reply and the connection closes.
