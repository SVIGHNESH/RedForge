# T1.01 RESP2 edge cases and protocol errors

**Item:** M1 | **Milestone:** MS1 | **Week:** 3
**Suggested owner:** Vivek | **Reviewer:** Vighnesh
**Effort:** 3 hours
**Depends on:** T0.05, T0.06
**Unblocks:** M1 done

## Goal

Malformed and unusual input never crashes the server, never desynchronises a connection, and produces the same reply Redis would.

## Context you need

- `HELLO` must fail so RESP3 negotiation does not happen. It is not in the command table, so the unknown-command error is the rejection.
- A protocol error is fatal for that connection only: reply `-ERR Protocol error: <detail>` then close after the write drains.

## Steps

1. Add cases to the reader tests and to an integration test in `test/integration/protocol_test.go`, sending raw bytes over a `net.Conn`:
   - null bulk string as an argument (`$-1`) inside a command array: reject with protocol error, Redis does too.
   - empty array `*0\r\n`: ignored, no reply, connection stays open.
   - bulk length with leading `+` or non-digit: protocol error `invalid bulk length`.
   - array length above the cap: protocol error `invalid multibulk length`.
   - a `\n` without `\r`: protocol error.
   - inline command longer than 64 KB: protocol error `too big inline request`.
   - `HELLO 3`: `-ERR unknown command 'HELLO'...`.
   - a command sent one byte at a time with `time.Sleep(1ms)` between bytes: one correct reply.
2. Assert that after a protocol error the socket reaches EOF within 100 ms.
3. Assert that a valid command sent on a different connection during the above still works, proving one bad client cannot stall others.

## Acceptance

- [ ] All listed cases pass locally and in CI.
- [ ] Fuzz corpus from T0.02 extended with the new malformed cases.
