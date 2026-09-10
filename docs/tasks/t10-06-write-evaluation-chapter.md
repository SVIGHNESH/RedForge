# T10.06 Write the evaluation chapter

**Item:** M10 | **Milestone:** MS10 | **Week:** 12 to 13
**Suggested owner:** Ritk drafts, all four review | **Reviewer:** Rajat
**Effort:** 4 hours
**Depends on:** T10.02 to T10.05, T8.04, T8.06
**Unblocks:** T11.04

## Goal

`docs/REPORT.md` evaluation chapter: setup, method, tables, charts, attribution, and the honest paragraph on what the single-threaded model cost in measured terms.

## Steps

1. Setup section copied from `spec.txt` plus the exact command lines from `cmdline.txt`.
2. Method section restating synopsis Section 7.2 in past tense with any deviations (reruns, Scenario 3 split invocation).
3. Paste the tables from `report.md`; embed the four charts.
4. Attribution section from `attribution.md`, one subsection per gap.
5. Durability cost paragraph from T10.03 and the `KEYS` stall paragraph from T10.04, each ending with the number that answers the viva question.
6. Limitations paragraph: single machine, loopback only, one Redis version.
7. Every sentence on its own line, no em dashes.

## Acceptance

- [ ] All four members have read it and can state the largest gap and its attribution without notes.
