# T11.02 README

**Item:** M11 | **Milestone:** MS11 | **Week:** 14
**Suggested owner:** Vighnesh | **Reviewer:** Ritk
**Effort:** 2 hours
**Depends on:** everything merged
**Unblocks:** submission

## Steps

1. Sections, in order: one-paragraph description with the bounded meaning of "compatible"; build (`make build`); run with every flag from MASTER-PLAN Section 3.5 and what each does; quick demo with `redis-cli`; supported commands (the closed list from MASTER-PLAN Section 1.2 with Must and Stretch marked and any cut item struck out); persistence guarantees in one paragraph each (which source wins, crash during `BGSAVE`, torn record, durability window); tests (`make test`, what it covers, CI); benchmark (`bench/run.sh` usage, link to results); Docker; documentation links (proposal, master plan, decisions, report); licence MIT; team.
2. Every claim in it must be something the test suite or the evaluation demonstrates. No aspirational statements.
3. Each sentence on its own line.

## Acceptance

- [ ] A member who has not touched the repo for a week can follow it from clone to a `redis-cli` session.
