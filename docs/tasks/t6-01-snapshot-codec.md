# T6.01 Snapshot codec

**Item:** M8 | **Milestone:** MS6 | **Week:** 5
**Suggested owner:** Rajat | **Reviewer:** Ritk
**Effort:** 3 hours
**Depends on:** T0.07, T4.02, T4.04, T4.05
**Unblocks:** T6.03, T6.04, T9.06

## Goal

`internal/snapshot` writes and reads the frozen binary format for every value type and rejects a file whose CRC fails.

## Context you need

Format from MASTER-PLAN Section 4.8, little-endian:

```
"RFSNAP" + version u8 (=1)
max_seq u64 | count u64
entries: key (u32 len + bytes) | type u8 | expire_at i64 | value
  STRING: u32 len + bytes
  LIST:   u32 n, then n items (u32 len + bytes) head to tail
  HASH:   u32 n, then n pairs (field, value)
  SET:    u32 n, then n members
  ZSET:   u32 n, then n pairs (member, score f64)   stretch
crc32 u32 over everything before it
```

## Steps

1. `Write(w io.Writer, maxSeq uint64, entries func(yield func(key string, e *store.Entry) bool)) error` wrapping `w` in a `crc32` hashing writer, writing header, entries, then the CRC. Use `bufio.Writer`.
2. `Read(r io.Reader) (maxSeq uint64, entries []KeyEntry, err error)` that reads everything, verifies the trailing CRC, and returns `ErrChecksum` on mismatch. Stream into the store in T6.04 rather than building a huge slice; provide a callback form `ReadInto(r, put func(key, *Entry))` and verify the CRC at the end, undoing nothing on failure (the caller starts with an empty store and discards it).
3. Tests: round trip one of each type including an expiring key; empty snapshot; flip one byte anywhere and assert `ErrChecksum`; truncated file returns an error.

## Acceptance

- [ ] Round trip and corruption tests pass.
- [ ] Format documented in `docs/CONTRACTS.md` matches the code.
