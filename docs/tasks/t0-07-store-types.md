# T0.07 Store types, Entry, Value, Clock, map-backed Store

**Item:** M3 (foundation) | **Milestone:** MS0 | **Week:** 2
**Suggested owner:** anyone | **Reviewer:** Ritk
**Effort:** 3 hours
**Depends on:** T0.01
**Unblocks:** T0.08, all data-type tasks

## Goal

`internal/store` provides the frozen `Store` interface with a single map-backed implementation, and the value types every data structure task will fill in.
No commands yet.

## Context you need

Frozen from MASTER-PLAN Sections 4.2 and 4.3:

```go
type Type uint8
const ( TString Type = iota + 1; TList; THash; TSet; TZSet )

type Value interface { Clone() Value }
type Entry struct { Type Type; Val Value; ExpireAt int64 }  // ExpireAt: absolute Unix ms, 0 = none

type Clock interface { NowMs() int64 }

type Store interface {
    Lookup(key string) (*Entry, bool)
    Mutable(key string) (*Entry, bool)
    Put(key string, e *Entry)
    Delete(key string) bool
    Exists(key string) bool
    Keys(pattern string) []string
    SetExpireAt(key string, atMs int64) bool
    Persist(key string) bool
    TTLms(key string) (int64, TTLStatus)   // NoKey, NoExpire, HasExpire
    Len() int
    ExpiringLen() int
    SweepExpired(sampleSize int, budget time.Duration) int
    Snapshot() SnapshotView
    ForEach(fn func(key string, e *Entry) bool)
}
```

## Steps

1. Create `types.go` with the types above and `StringVal{B []byte}`, plus empty stubs `ListVal`, `HashVal{M map[string][]byte}`, `SetVal{M map[string]struct{}}`, `ZSetVal` (stub). Each gets `Clone()`; `StringVal.Clone` returns itself because strings are never mutated in place.
2. Create `store.go` with `type mapStore struct { m map[string]*Entry; expires map[string]int64; clock Clock; snapshotActive bool }` and `New(clock Clock) Store`.
3. Implement `Lookup`, `Put`, `Delete`, `Exists`, `Len`, `ForEach`. `Lookup` and `Mutable` call a private `expireIfDue(key, e)` that deletes and returns false when `ExpireAt > 0 && ExpireAt <= clock.NowMs()`. T3.01 fills in the rest of the expiry methods; leave `SetExpireAt`, `Persist`, `TTLms`, `SweepExpired` as TODO returning zero values, and `Snapshot` returning nil for T6.02.
4. `Mutable` is identical to `Lookup` for now, with a comment pointing at T6.02 for the copy-on-write behaviour.
5. `Keys` in `glob.go`: implement Redis glob matching (`*`, `?`, `[abc]`, `[a-z]`, `[^a]`, backslash escape). Table tests copied from Redis's `stringmatchlen` cases.
6. `RealClock` in `clock.go` using `time.Now().UnixMilli()`, and `FakeClock` with `Advance(d)` in `clock_test.go` exported through an `_test` helper package so other packages' tests can use it.
7. Unit tests for every implemented method and for glob.

## Acceptance

- [ ] Glob tests pass including `h[^e]llo`, `h[a-b]llo`, `h\*llo`.
- [ ] `Lookup` on a key with `ExpireAt` in the past returns absent and the key is gone.
- [ ] Interface compliance: `var _ Store = (*mapStore)(nil)` compiles.
