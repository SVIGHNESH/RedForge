# T10.02 Run Scenarios 1 to 4 on both servers

**Item:** M10 | **Milestone:** MS10 | **Week:** 12
**Suggested owner:** Ritk, with one other member watching | **Reviewer:** Rajat
**Effort:** 3 to 4 hours of machine time
**Depends on:** T10.01, T8.01
**Unblocks:** T10.06

## Goal

Complete, matched, three-run results for the read baseline, write baseline, mixed workload and large payload scenarios.

## Steps

1. For scenario in 1 to 4: `bench/run.sh --target rfs --scenario N` then `bench/run.sh --target redis --scenario N`. Alternate targets per scenario so thermal drift affects both.
2. After each scenario, run the report tool and check spread. If any metric's spread exceeds 10 percent, rerun that scenario once and keep the run with lower spread; note the rerun in `notes.md`.
3. Record anything unusual (a background process, a thermal warning) in `bench/results/<date>/notes.md` with a timestamp.
4. Commit the results directory.

## Acceptance

- [ ] Every cell of the Scenario 1 to 4 tables is filled for both targets with median and spread.
- [ ] Spread under 10 percent on rps and p99 for every scenario, or the exception is explained in `notes.md`.
