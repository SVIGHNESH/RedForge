# T4.01 ListVal deque

**Item:** M5 | **Milestone:** MS4 | **Week:** 3
**Suggested owner:** Ritk | **Reviewer:** Vighnesh
**Effort:** 2 to 3 hours
**Depends on:** T0.07
**Unblocks:** T4.02

## Goal

A ring-buffered deque giving O(1) amortised push and pop at both ends, with `Clone()` for the snapshot copy-on-write path.

## Context you need

Complexity claims in the synopsis Table 3.1: `LPUSH`/`RPUSH`/`LPOP`/`RPOP`/`LLEN` O(1), `LRANGE` O(S+N).
A Go slice with `append` at the front would be O(N), so the deque is required, not optional.

## Steps

1. `type ListVal struct { buf [][]byte; head, n int }` in `internal/store/list.go`.
2. Methods: `PushFront`, `PushBack`, `PopFront`, `PopBack`, `Len`, `At(i)`, `Range(start, stop) [][]byte` with Redis negative-index rules (`-1` is last, out-of-range clamps, `start > stop` gives empty), `Clone`.
3. Grow by doubling when full, copying in two segments around the wraparound.
4. Shrink is not required within scope; note it in the doc comment.
5. Tests: push and pop at both ends across several wraparounds, `Range` for every combination of positive, negative and out-of-range indices against a plain slice reference, `Clone` independence (mutate the clone, original unchanged).

## Acceptance

- [ ] `go test -bench BenchmarkPushFront` shows constant time per op across 10^3 to 10^6 elements.
- [ ] Reference comparison test passes for 10000 random operations.
