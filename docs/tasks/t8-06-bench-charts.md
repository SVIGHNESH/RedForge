# T8.06 Charts from report.csv

**Item:** M10 | **Milestone:** MS8 | **Week:** 7 (skeleton), Week 12 (final)
**Suggested owner:** Ritk | **Reviewer:** Rajat
**Effort:** 2 hours
**Depends on:** T8.04
**Unblocks:** T10.06

## Goal

The charts for the report: throughput per scenario, latency percentiles per scenario, and the Scenario 5 client sweep, each with both servers on the same axes.

## Steps

1. `bench/plot/plot.py` using matplotlib (this is report tooling, not the server, so the standard-library-only rule does not apply; note that in the README).
2. Chart 1: grouped bars, requests per second, scenarios 1 to 4, two bars per scenario.
3. Chart 2: grouped bars per scenario for p50, p95, p99, p99.9, log scale on the y axis.
4. Chart 3: lines, requests per second against client count 1, 10, 50, 200 for Scenario 5, four lines: ours and Redis at `always` and `everysec`.
5. Chart 4: `KEYS` stall against keyspace size, log-log, two lines.
6. Output SVG and PNG into `bench/results/<date>/charts/`.
7. One consistent palette; label axes with units; include the machine's CPU model in a footnote.

## Acceptance

- [ ] All four charts render from the fixture data used in T8.04.
