# T2.02 INCR

**Item:** M3 | **Milestone:** MS2 | **Week:** 3 to 4
**Suggested owner:** Vighnesh | **Reviewer:** Rajat
**Effort:** 1 to 2 hours
**Depends on:** T2.01
**Unblocks:** T3.04

## Goal

`INCR` matches Redis integer rules exactly, because the 100-client lost-update test and the replay tests depend on it.

## Context you need

- Redis parses the value as a signed 64-bit integer with `strconv.ParseInt(s, 10, 64)` semantics: no leading `+`, no spaces, no leading zeros except `0` itself, and `-0` is invalid.
- Missing key counts as 0. Overflow returns `ERR increment or decrement would overflow`.
- Non-integer value returns `ERR value is not an integer or out of range`.
- Propagate `INCR k`, not `SET k <new>`; replay recomputes the same result and keeps the log small.

## Steps

1. Register `INCR` (arity 2, Write).
2. Implement with `store.Lookup`; on `WRONGTYPE` reply the error; parse, check overflow with `math.MaxInt64`, `Put` a new `StringVal` (do not mutate the old bytes), propagate, reply integer.
3. Table tests: missing key, `"007"`, `"+1"`, `" 1"`, `"9223372036854775807"`, `"abc"`, list key.

## Acceptance

- [ ] All table cases match what `redis-cli` against official Redis returns (check two of them by hand and paste in the PR).
