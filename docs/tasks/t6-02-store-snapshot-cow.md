# T6.02 Store.Snapshot clone and copy-on-write in Mutable

**Item:** M8 | **Milestone:** MS6 | **Week:** 5
**Suggested owner:** Rajat | **Reviewer:** Ritk
**Effort:** 2 to 3 hours
**Depends on:** T0.07, T4.02
**Unblocks:** T6.03

## Goal

The store can hand out a point-in-time view that a background goroutine may read while the loop keeps mutating, with no lock and no data race.

## Context you need

MASTER-PLAN Section 4.6. The mechanism:

- `Snapshot()` copies the top-level `map[string]*Entry` into a new map (entries shared), sets `snapshotActive = true`, and returns the clone as a `SnapshotView` with `ForEach` and `Release()`.
- While `snapshotActive`, `Mutable(key)` replaces the live entry with a copy whose `Val` is `Val.Clone()`, so the clone's shared entry is never mutated. `Put` and `Delete` only touch the live map.
- `Release()` clears `snapshotActive` and drops the clone. Called from the loop thread on completion.

## Steps

1. Implement as above. Measure the clone duration and expose it through `Store.LastCloneDuration()` for INFO `last_bgsave_clone_ms`.
2. `Mutable` copy-on-write: only clone once per key per snapshot. Track with a generation counter on the entry (`e.gen == snapshotGen` means already cloned this snapshot).
3. Unit test: fill store, take snapshot, `Mutable` a list and push, `Put` a new string, `Delete` another; iterate the view and assert the old list contents, the deleted key present, the new key absent.
4. Race test: run the view's `ForEach` in a goroutine while the test mutates through `Mutable` and `Put`, under `go test -race`.

## Acceptance

- [ ] `go test -race ./internal/store` clean.
- [ ] Clone of 10^6 keys measured and noted in the PR (expect tens of milliseconds).
