# T10.05 Attribution experiments

**Item:** M10 | **Milestone:** MS10 | **Week:** 12
**Suggested owner:** Vighnesh and Rajat | **Reviewer:** Ritk
**Effort:** 3 to 4 hours
**Depends on:** T10.02, T10.03
**Unblocks:** T10.06

## Goal

For each gap between our server and Redis, evidence pointing at a specific architectural decision, per synopsis Section 7.2's candidate list: garbage collector, no small-object allocator, no compact encodings, per-command allocation in the codec.

## Steps

1. GC share: rerun Scenario 2 on our server with `GOGC=off` (memory will grow; keep the run short with `-n 300000`) and with `GOGC=400`. The throughput and p99 delta against the default is the GC attribution. Also capture `GODEBUG=gctrace=1` output for one run and count pauses.
2. Codec allocation: `go test -bench . -benchmem ./internal/resp` and `./internal/command` gives allocations per parsed command and per reply. Multiply by requests per second to get allocation rate; compare against the GC pause count.
3. Syscall share: run Scenario 1 under `strace -c -p <pid>` for 10 s (this slows the server, so only compare ratios) to see `epoll_wait`, `read`, `write` counts per request; compare with Scenario 5's pipelined numbers where syscalls per command drop by up to 16 times.
4. Memory footprint: compare peak RSS per key at 10^6 keys between the servers from the stall runs. The difference is the missing compact encoding and Go map overhead.
5. Write each finding as: gap observed, experiment, result, decision responsible, in `bench/results/<date>/attribution.md`.

## Acceptance

- [ ] Every gap larger than 20 percent in the scenario tables has an attribution entry backed by one of the experiments above.
