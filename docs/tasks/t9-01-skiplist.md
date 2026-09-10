# T9.01 Skiplist for sorted sets (Stretch S2)

**Item:** S2 | **Milestone:** MS9 | **Week:** 8
**Suggested owner:** Ritk | **Reviewer:** Vighnesh
**Effort:** 4 to 5 hours
**Depends on:** T0.07; all of Ritk's Must items merged
**Unblocks:** T9.02

## Goal

Pugh's skiplist ordered by score then member, with rank queries, as the backing structure for `ZSetVal`.

## Context you need

- Max level 32, promotion probability 0.25, as Redis uses.
- Each forward pointer carries a `span` (number of elements skipped) so rank is computed during search in O(log N).
- Ordering: by score ascending, ties broken by member bytes ascending.
- `ZSetVal` pairs the skiplist with `map[string]float64` for O(1) score lookup.

## Steps

1. `internal/store/skiplist.go`: `Insert(member, score)`, `Delete(member, score)`, `Rank(member, score) int` (0-based), `RangeByRank(start, stop) []Element`, `Len`, plus an iterator.
2. Level generation with a seeded `math/rand` so tests are deterministic.
3. `ZSetVal` with `Add(member, score) (added bool)` that deletes and reinserts on score change, `Score(member)`, `Rank`, `Range`, `Clone` (rebuild from iteration, O(N)).
4. Tests against a sorted-slice reference: 10000 random inserts, updates and deletes, comparing full order and every rank after each 100 operations; range with negative indices; duplicate scores ordered by member.

## Acceptance

- [ ] Reference test passes with three seeds.
- [ ] Insert benchmark shows sub-linear growth from 10^3 to 10^6.
