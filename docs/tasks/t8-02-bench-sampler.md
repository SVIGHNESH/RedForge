# T8.02 CPU and memory sampler

**Item:** M10 | **Milestone:** MS8 | **Week:** 7
**Suggested owner:** Ritk | **Reviewer:** Rajat
**Effort:** 1 to 2 hours
**Depends on:** nothing (standalone script)
**Unblocks:** T8.01

## Goal

Peak CPU utilisation and peak resident memory of the server process during a run, from `/proc`, with no external tools.

## Steps

1. `bench/sample.sh PID OUTFILE`: every 100 ms read `/proc/PID/stat` fields 14 and 15 (utime and stime, in clock ticks) and `/proc/PID/status` `VmHWM` and `VmRSS`; compute CPU percent over the interval as `delta_ticks / CLK_TCK / 0.1 * 100`; append a line `timestamp,cpu_pct,rss_kb,hwm_kb`.
2. Stop when PID exits or on SIGTERM.
3. `bench/peaks.sh OUTFILE` prints `peak_cpu_pct` and `peak_rss_kb` (use `VmHWM` from the last line for peak RSS).
4. Test by hand on a `yes > /dev/null` process (expect around 100 percent) and on an idle shell (expect around 0).

## Acceptance

- [ ] Output file has one line per 100 ms for the run duration.
- [ ] Peak CPU of our server under Scenario 1 is close to 100 percent, which is the single-core bound made visible.
