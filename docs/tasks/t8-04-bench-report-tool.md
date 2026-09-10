# T8.04 Report tool: medians, spread, Markdown tables

**Item:** M10 | **Milestone:** MS8 | **Week:** 7
**Suggested owner:** Ritk | **Reviewer:** Rajat
**Effort:** 3 hours
**Depends on:** T8.01, T8.02
**Unblocks:** T8.06, T10.06

## Goal

A Go program reads a results directory and emits the comparison tables the report needs, with median and spread per metric, for both targets side by side.

## Context you need

`redis-benchmark --csv` output columns in 7.x: `test`, `rps`, `avg_latency_ms`, `min_latency_ms`, `p50_latency_ms`, `p95_latency_ms`, `p99_latency_ms`, `max_latency_ms`.
p99.9 is not in the CSV. Use `--precision 3` and parse the human output's latency histogram, or pass `-l`? Neither gives p99.9 directly. Use `redis-benchmark ... 2>&1` without `--csv` for one of the three runs and parse the `99.900%` line from its latency percentile summary, which 7.x prints. Record which run supplied p99.9.

## Steps

1. `bench/cmd/report/main.go` (standard library only): walk `bench/results/<date>/`, group files by target, scenario, sub-case; parse CSV and the percentile text; attach `peak_cpu_pct` and `peak_rss_kb` from the sampler file of the same run.
2. For each metric compute median of the three runs and spread as `(max - min) / median` in percent.
3. Emit `report.md` with one table per scenario: rows are metrics, columns are `rfs median`, `rfs spread`, `redis median`, `redis spread`, `ratio`.
4. Emit `report.csv` flat for the plotting script.
5. Unit test on a fixture directory with synthetic CSVs.

## Acceptance

- [ ] `go run ./bench/cmd/report bench/results/<date>` prints tables for every scenario present.
- [ ] Missing runs are reported, not silently skipped.
