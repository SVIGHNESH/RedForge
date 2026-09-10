# T1.03 Test harness: minimal RESP client

**Item:** M9 | **Milestone:** MS1 | **Week:** 3 to 4
**Suggested owner:** Vivek | **Reviewer:** Rajat
**Effort:** 2 hours
**Depends on:** T0.02, T0.03
**Unblocks:** concurrency tests, crash tests

## Goal

Tests need a client that can open hundreds of connections, pipeline, and read typed replies.
`redis-cli` is kept for compatibility sessions; this client is for load and ordering tests.

## Steps

1. `type Client struct { conn net.Conn; r *bufio.Reader; w *bufio.Writer }` with `Dial(addr)`.
2. `Do(args ...string) (resp.Value, error)` writes one command array and reads one reply using `resp.Parse` over a buffered reader. Handle `ErrIncomplete` by reading more.
3. `Pipeline(cmds [][]string) ([]resp.Value, error)` writes all, flushes once, reads all.
4. Helpers: `MustDo(t, ...)`, `MustInt`, `MustBulk`, `MustNil`, `MustError(prefix)`.
5. Test the client against the running server: `PING`, `ECHO`, pipeline of 100 `PING`s.

## Acceptance

- [ ] 100 clients opened in a loop and closed leak no goroutines in the test process.
- [ ] Pipeline of 1000 commands returns 1000 replies in order.
