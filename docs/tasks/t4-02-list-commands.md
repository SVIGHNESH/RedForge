# T4.02 List commands

**Item:** M5 | **Milestone:** MS4 | **Week:** 3 to 4
**Suggested owner:** Ritk | **Reviewer:** Vighnesh
**Effort:** 3 hours
**Depends on:** T4.01, T0.06, T0.08
**Unblocks:** T4.03, T6.01

## Goal

`LPUSH`, `RPUSH`, `LPOP`, `RPOP`, `LLEN`, `LRANGE` with Redis semantics, using `Store.Mutable` for every in-place change.

## Context you need

- Every mutation of a collection goes through `store.Mutable(key)`, never through a stale pointer, because T6.02 makes `Mutable` clone the value during a snapshot. If you touch the map or the value any other way, the snapshot test will catch it later and the fix is yours.
- `LPUSH k a b c` pushes a, then b, then c, so the list reads `c b a`. Multi-element push is in scope because `redis-cli` and redis-benchmark use it.
- `LPOP`/`RPOP` take one key only (no count argument). A pop that empties the list deletes the key.
- `LRANGE` on a missing key returns an empty array. `LLEN` on missing returns 0. Wrong type returns `WRONGTYPE`.
- Propagate the command as received for pushes; for pops propagate `LPOP k` / `RPOP k` only when something was popped.

## Steps

1. `internal/command/lists.go`: register the six commands with arities `-3, -3, 2, 2, 2, 4`.
2. Helper `listForWrite(ctx, key) (*ListVal, resp.Reply)` that creates the key when missing and returns `WRONGTYPE` on other types; `listForRead` that returns nil for missing.
3. Implement each; delete the key on empty after pop.
4. Unit tests with the `memoryAppender` for propagated forms.
5. `redis-cli` session test using `testutil.CLI`: `LPUSH l a b`, `RPUSH l c`, `LRANGE l 0 -1` expecting `b a c`, `LPOP l`, `RPOP l`, `LLEN l`, `RPOP l`, `EXISTS l` expecting 0.

## Acceptance

- [ ] Session test passes.
- [ ] `grep -n "\.m\[" internal/command/lists.go` finds nothing (no direct map access).
