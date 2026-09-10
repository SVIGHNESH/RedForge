# T3.02 Active expiry sweep on the loop tick

**Item:** M4 | **Milestone:** MS3 | **Week:** 5 to 6
**Suggested owner:** Vighnesh | **Reviewer:** Rajat
**Effort:** 2 to 3 hours
**Depends on:** T3.01, T0.04
**Unblocks:** T7.04 (EX4)

## Goal

Keys that expire and are never touched again are reclaimed by a bounded sweep that runs inside the event loop, so the single-threaded invariant holds.

## Context you need

- The loop's `EpollWait` timeout is the time to the next 100 ms tick (MASTER-PLAN Section 3.3). If no tick mechanism exists yet, add it in this task: `nextTick` timestamp, timeout computed each iteration, `onTick()` called when due.
- Algorithm, adapted from Redis: sample up to 20 keys from `expires`, delete the expired ones, and repeat while more than 25 percent of the sample was expired, stopping when 1 ms of budget is spent.
- Go map iteration starts at a random position, so "sample 20" is "range over `expires` and break after 20".

## Steps

1. Implement `SweepExpired(sampleSize int, budget time.Duration) int` in the store per the algorithm, using the injected clock for expiry checks and `time.Now()` only for the budget.
2. Add the tick to the loop and call `store.SweepExpired(20, time.Millisecond)` from `onTick`. Add `total_sweep_deleted` to the store counters.
3. Unit test with `FakeClock`: insert 10000 keys expiring at t+1, advance clock, call `SweepExpired` repeatedly, assert all gone and each call took under 5 ms wall time.
4. Unit test: with 0 expired keys the sweep does one sample and stops.

## Acceptance

- [ ] Integration: 10000 keys with `EXPIRE 1`, never accessed, `INFO keyspace` shows `keys=0` within 5 s.
- [ ] `PING` latency measured during the sweep test stays under 2 ms.
