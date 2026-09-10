# T5.01 Log record codec

**Item:** M7 | **Milestone:** MS5 | **Week:** 3
**Suggested owner:** Rajat | **Reviewer:** Ritk
**Effort:** 2 to 3 hours
**Depends on:** T0.01, T0.03
**Unblocks:** T5.02, T5.03, T9.07

## Goal

`internal/aof` can encode a record and decode a stream of records, reporting exactly where a torn or corrupt record starts.

## Context you need

Frozen format from MASTER-PLAN Section 4.7, little-endian:

```
file header   "RFAOF" + version u8 (=1)
record        seq u64 | len u32 | crc32 u32 | payload[len]
payload       RESP2 array of the propagated command
crc32         IEEE over seq || len || payload
```

## Steps

1. `Encode(dst []byte, seq uint64, args [][]byte) []byte` appends one record. Build the RESP array with `resp.ArrayOfBulks(args).AppendTo`.
2. `type Reader struct{...}` over an `io.Reader` with `Next() (seq uint64, args [][]byte, err error)`. On a clean EOF at a record boundary return `io.EOF`. On a truncated header or payload return `ErrTorn`. On CRC mismatch return `ErrCorrupt`. In both error cases expose `Reader.LastGoodOffset()` as the byte offset where the failing record started.
3. `WriteHeader(w)` and `ReadHeader(r)` with a version check.
4. Tests: round trip; for a file of three records, truncate at every byte offset from the start of record 3 to its end and assert `Next` returns two records then `ErrTorn` with `LastGoodOffset` equal to record 3's start; flip one payload byte and assert `ErrCorrupt`; flip one byte in `len` and assert either error but never a panic or a huge allocation (cap `len` at 512 MB before allocating).

## Acceptance

- [ ] Every truncation offset test passes.
- [ ] `go test -fuzz FuzzReader` for 30 s with no panic.
