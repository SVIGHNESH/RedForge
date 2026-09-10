# T0.03 RESP2 writer and reply types

**Item:** M1 | **Milestone:** MS0 | **Week:** 1
**Suggested owner:** anyone | **Reviewer:** Vivek
**Effort:** 2 hours
**Depends on:** T0.01
**Unblocks:** T0.06

## Goal

Handlers return a `resp.Reply` and the loop serialises it into a connection's write buffer without allocating a new slice per reply where avoidable.

## Context you need

Handlers return replies, they never write to sockets.
Signature fixed in MASTER-PLAN Section 4.4: `type Handler func(ctx *Ctx, args [][]byte) resp.Reply`.

## Steps

1. Define `type Reply interface { AppendTo(dst []byte) []byte }`.
2. Implement constructors: `SimpleString(s)`, `Error(s)`, `Integer(n)`, `Bulk(b []byte)`, `NullBulk()`, `Array(items ...Reply)`, `NullArray()`, `EmptyArray()`.
3. Add common singletons: `OK`, `PONG`, `Nil`.
4. Add `BulkString(s string)` and `ArrayOfBulks(items [][]byte)` helpers because most collection replies are arrays of bulk strings.
5. `AppendTo` uses `strconv.AppendInt` and direct byte appends. No `fmt`.
6. Tests: each constructor's exact bytes, nested arrays, empty bulk (`$0\r\n\r\n`), and a round trip through `Parse` from T0.02 if it has merged (otherwise assert bytes only).
7. Add `BenchmarkAppendBulk` so allocation per reply is visible for the evaluation later.

## Acceptance

- [ ] Byte-exact tests pass for every reply type.
- [ ] `go test -bench . -benchmem` shows 0 allocations for `Integer` and `SimpleString` into a preallocated buffer.
