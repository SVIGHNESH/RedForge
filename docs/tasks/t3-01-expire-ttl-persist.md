# T3.01 Expiry storage, EXPIRE, TTL, PERSIST, PEXPIREAT, passive reclamation

**Item:** M4 | **Milestone:** MS3 | **Week:** 5
**Suggested owner:** Vighnesh | **Reviewer:** Rajat
**Effort:** 3 to 4 hours
**Depends on:** T0.07, T0.08, T2.01
**Unblocks:** T3.02, T3.03, T5.03, T7.04

## Goal

Keys carry an absolute expiry in Unix milliseconds, the three expiry commands work, and the log receives `PEXPIREAT` never `EXPIRE`.

## Context you need

Proposal Section 6.3 is the reason for all of this: a relative TTL in a snapshot would resurrect lifetime after a restart.

Redis semantics:

- `EXPIRE k seconds` returns 1 if the key exists and the TTL was set, 0 otherwise. A non-positive TTL deletes the key immediately (and propagates `DEL`).
- `TTL k` returns `-2` for a missing key, `-1` for a key without expiry, otherwise seconds rounded up.
- `PERSIST k` returns 1 if a TTL was removed, 0 otherwise.
- `PEXPIREAT k ms` sets the absolute time. It exists because the log stores it; accept it from clients too but do not advertise it.
- `SET` clears an existing TTL. Confirm T2.01 does this (it should, since `Put` replaces the entry).

## Steps

1. In `internal/store`: implement `SetExpireAt` (writes `Entry.ExpireAt` and `expires[key]`; a `Put` or `Delete` must also remove the key from `expires`), `Persist`, `TTLms`, `ExpiringLen`. Make `Put` clear `expires[key]` when the new entry has `ExpireAt == 0`.
2. Passive path already exists in `Lookup`; extend it to also remove from `expires` and to increment a store-level `ExpiredCount` that INFO reads as `expired_keys`.
3. `internal/command/expire.go`: register `EXPIRE` (3, Write), `PEXPIREAT` (3, Write), `TTL` (2, ReadOnly), `PERSIST` (2, Write).
4. `EXPIRE` computes `at := ctx.Now + seconds*1000`, calls `SetExpireAt`, propagates `PEXPIREAT k at`. `PERSIST` propagates itself only when it returns 1.
5. Unit tests with `FakeClock`: set, advance 999 ms, still present; advance 1 more, absent on `Lookup`; `TTL` rounding; `PERSIST` on no-TTL key returns 0 and does not propagate; propagated form of `EXPIRE` contains an absolute value equal to `now + 10000`.

## Acceptance

- [ ] `redis-cli`: `SET s 1`, `EXPIRE s 100`, `TTL s` prints `100` or `99`, `PERSIST s`, `TTL s` prints `-1`.
- [ ] The `memoryAppender` never sees a record starting with `EXPIRE`.
