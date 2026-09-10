# T8.01 Benchmark runner and scenario config

**Item:** M10 | **Milestone:** MS8 | **Week:** 7
**Suggested owner:** Ritk | **Reviewer:** Rajat
**Effort:** 4 hours
**Depends on:** T1.05 (binary builds), official Redis installed locally
**Unblocks:** T8.02 to T8.06, MS10

## Goal

One script runs any scenario from synopsis Table 7.1 against either server with matched settings, a warm-up, three timed runs, and a server restart with a fresh directory between runs.

## Context you need

Scenarios:

| No | redis-benchmark arguments | Notes |
|---|---|---|
| 1 | `-t get -d 64 -c 50 -n 1000000 -P 1` | read baseline |
| 2 | `-t set -d 64 -c 50 -n 1000000 -P 1` | write baseline, log on |
| 3 | two concurrent processes: `-t get -c 40` and `-t set -c 10`, both `-d 64 -P 1` | 80/20 mix |
| 4 | `-t set -d 4096 -c 50 -n 300000 -P 1` | payload size |
| 5 | `-t set -d 64 -P 16 -c {1,10,50,200}` repeated at `--appendfsync always` and `everysec` | sweep |

Server flags: ours `--port P --dir TMP --appendfsync POLICY`; Redis `redis-server --port P --dir TMP --save "" --appendonly yes --appendfsync POLICY --bind 127.0.0.1 --io-threads 1`.

## Steps

1. `bench/scenarios.conf`: one line per scenario and sub-case with fields `id`, `policy`, `benchmark_args`, and for scenario 3 two argument sets.
2. `bench/run.sh --target rfs|redis --scenario N [--all]`: for each sub-case, do warm-up plus three runs; per run: create a temp dir, start the server pinned with `taskset -c 2`, wait for `PING`, start `bench/sample.sh` (T8.02) on the server PID, run `redis-benchmark -p P --csv` pinned with `taskset -c 0,1`, stop sampler, stop server, save CSV to `bench/results/<date>/<target>/s<N>-<subcase>-run<k>.csv`.
3. Write the exact server command line and benchmark command line into `cmdline.txt` next to the CSVs.
4. Refuse to run if `redis-server` or `redis-benchmark` is missing, printing the install hint.

## Acceptance

- [ ] `bench/run.sh --target rfs --scenario 1` produces four CSVs (warm-up plus three) and a `cmdline.txt`.
- [ ] Same for `--target redis`.
