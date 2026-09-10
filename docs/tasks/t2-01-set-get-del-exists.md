# T2.01 SET, GET, DEL, EXISTS

**Item:** M3 | **Milestone:** MS2 | **Week:** 3
**Suggested owner:** Vighnesh | **Reviewer:** Rajat
**Effort:** 2 to 3 hours
**Depends on:** T0.06, T0.07, T0.08
**Unblocks:** T2.02, T2.04, all persistence tests

## Goal

The four basic string commands with Redis semantics, propagated correctly.

## Context you need

- `SET k v` only. No `EX`, `PX`, `NX`, `XX` options; they are outside the command matrix. Extra arguments return the wrong-arguments error.
- `SET` on a key of another type overwrites it and clears any TTL, as in Redis.
- `GET` on a non-string returns `WRONGTYPE Operation against a key holding the wrong kind of value`.
- `DEL` and `EXISTS` take one or more keys and return counts. `DEL` of zero keys does not propagate.
- Passive expiry happens inside `Store.Lookup`, so these handlers do not check TTL themselves.

## Steps

1. `internal/command/strings.go`: register `SET` (arity 3, Write), `GET` (2, ReadOnly). `internal/command/keys.go`: `DEL` (-2, Write), `EXISTS` (-2, ReadOnly).
2. `SET`: `store.Put(k, &Entry{Type: TString, Val: &StringVal{B: copy of v}})`, `Propagate("SET", k, v)`, reply `OK`. Copy the value bytes because the read buffer is reused.
3. `DEL`: count successful `store.Delete`; if count > 0 propagate `DEL` with only the keys that existed; reply integer.
4. Unit tests with the `memoryAppender` from T0.08 asserting the propagated forms, and `WRONGTYPE` after T4.02 lands (leave a TODO test if lists are not merged yet).

## Acceptance

- [ ] `redis-cli` session: `SET a 1`, `GET a`, `EXISTS a b`, `DEL a b` prints `OK`, `"1"`, `1`, `1`.
- [ ] `GET` of a missing key prints `(nil)`.
- [ ] Propagation tests pass.
