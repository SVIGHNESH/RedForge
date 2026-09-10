# T9.04 SUBSCRIBE, UNSUBSCRIBE, PUBLISH (Stretch S5)

**Item:** S5 | **Milestone:** MS9 | **Week:** 8
**Suggested owner:** Vivek | **Reviewer:** Vighnesh
**Effort:** 3 hours
**Depends on:** T0.05, T0.06; all of Vivek's Must items merged
**Unblocks:** S5 done

## Goal

Channel-based publish and subscribe, with subscriber mode on the connection, and nothing written to the log.

## Context you need

- `SUBSCRIBE ch [ch ...]` replies with one array per channel: `subscribe`, channel, count. The client is then in subscriber mode and may only send `SUBSCRIBE`, `UNSUBSCRIBE`, `PING`, `QUIT`; anything else gets `ERR Can't execute 'get': only (P|S)SUBSCRIBE / (P|S)UNSUBSCRIBE / PING / QUIT / RESET are allowed in this context`.
- `UNSUBSCRIBE` with no args unsubscribes all; when the count reaches 0 the client leaves subscriber mode.
- `PUBLISH ch msg` returns the number of clients that received it; the message is `*3 message ch msg` written straight into each subscriber's write buffer. Not propagated.
- Pattern subscribe is out of scope.
- A subscriber that disconnects must be removed from every channel set.

## Steps

1. `internal/command/pubsub.go` with `map[string]map[*server.Client]struct{}` on the server and a per-client `subs map[string]struct{}`.
2. Register the three commands with `PubSub` flag and `NoTx`.
3. Hook client close in `server` to call an `OnClose` callback that pubsub uses to clean up.
4. Tests: two `testutil.Client`s subscribe, a third publishes, both receive; unsubscribe one, publish again, count is 1; cross-process test with `redis-cli SUBSCRIBE` in the background and `redis-cli PUBLISH`.

## Acceptance

- [ ] Cross-process test passes.
- [ ] Log file does not grow on `PUBLISH`.
