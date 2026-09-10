# T8.05 KEYS event-loop stall measurement

**Item:** M10 | **Milestone:** MS8 | **Week:** 7
**Suggested owner:** Ritk | **Reviewer:** Rajat
**Effort:** 2 hours
**Depends on:** T2.03, T8.01
**Unblocks:** MS10

## Goal

Stall time of the event loop during `KEYS *` against 10^4, 10^5 and 10^6 keys, on both servers, quantifying Proposal Section 6.2.

## Steps

1. `bench/keys-stall.sh --target rfs|redis --keys N`: start the server; generate N `SET key:<i> x` lines and load with `redis-cli --pipe`; wait for `INFO keyspace` to show N keys.
2. Probe: a small Go program `bench/cmd/probe` that opens one connection and sends `PING` every 1 ms, recording each round trip. Start it, then on a second connection run `redis-cli KEYS '*' > /dev/null`, then stop the probe.
3. Report the maximum probe round trip as the stall, and on our server also read INFO `keys_last_iter_us` and `keys_last_reply_bytes` to split iteration from serialisation.
4. Three runs per size, median reported.
5. Memory note: 10^6 keys needs several hundred MB on each server; check `free` first.

## Acceptance

- [ ] Table with rows 10^4, 10^5, 10^6 and columns stall (ours), stall (Redis), iter_us (ours) exists in `bench/results/<date>/keys-stall.md`.
