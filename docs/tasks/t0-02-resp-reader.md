# T0.02 RESP2 reader

**Item:** M1 | **Milestone:** MS0 | **Week:** 1
**Suggested owner:** anyone (whole team learns from this one) | **Reviewer:** Vivek
**Effort:** 3 to 4 hours
**Depends on:** T0.01
**Unblocks:** T0.05, T0.06

## Goal

`internal/resp` can parse a complete RESP2 frame from a byte slice and report how many bytes it consumed, or report that more bytes are needed.
The event loop relies on the "need more" answer to support partial reads and pipelining.

## Context you need

RESP2 types and their wire form:

```
+OK\r\n                      simple string
-ERR message\r\n             error
:42\r\n                      integer
$5\r\nhello\r\n              bulk string      $-1\r\n is the null bulk string
*2\r\n$4\r\nPING\r\n$3\r\nfoo\r\n   array    *-1\r\n is the null array
PING foo\r\n                 inline command, split on spaces, used by nc and telnet
```

Clients send commands only as arrays of bulk strings or as inline lines.
The reader still needs to parse all five types because tests and the replication stream reuse it.

## Steps

1. Define `type Value struct { Kind Kind; Str []byte; Int int64; Array []Value; Null bool }` with `Kind` as `SimpleString`, `Error`, `Integer`, `BulkString`, `Array`.
2. Write `func Parse(buf []byte) (v Value, n int, err error)`. Return `n == 0` and `err == ErrIncomplete` when the frame is not fully present. Return `ErrProtocol` for malformed input, with a message that starts with `Protocol error:`.
3. Handle inline commands: if the first byte is not one of `+ - : $ *`, read up to `\r\n` and split on spaces into an array of bulk strings.
4. Reject bulk lengths above 512 MB and array lengths above 1024 * 1024 with `ErrProtocol`.
5. Write `func ParseCommand(buf []byte) (args [][]byte, n int, err error)` that accepts only an array of bulk strings or an inline line and returns the argument vector.
6. Table-driven tests for every type, null forms, nested arrays, inline, and every truncation point of a sample frame (loop over prefixes of the bytes and assert `ErrIncomplete`).
7. Add `FuzzParse` under `go test -fuzz` that asserts the parser never panics and never returns `n > len(buf)`.

## Acceptance

- [ ] Every table case passes, including every prefix truncation.
- [ ] 30 seconds of fuzzing produce no panic.
- [ ] `ParseCommand` on `"*2\r\n$4\r\nPING\r\n$3\r\nfoo\r\n*1\r\n$4\r\nPING\r\n"` returns the first command with `n` equal to the length of the first frame only.
