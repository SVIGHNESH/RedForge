# T0.11 Contracts freeze

**Item:** all | **Milestone:** MS0 | **Week:** 2 (last task of the phase)
**Suggested owner:** Vighnesh | **Reviewer:** all four must approve
**Effort:** 1 hour plus the meeting
**Depends on:** T0.02 to T0.08 merged
**Unblocks:** Phase 1

## Goal

The contracts every track depends on are copied out of MASTER-PLAN into `docs/CONTRACTS.md`, checked against the merged code, and frozen by a PR that all four approve.

## Steps

1. Copy MASTER-PLAN Section 4 into `docs/CONTRACTS.md`.
2. For each Go snippet in it, open the merged source and make the document match the code exactly (names, argument order, comments). Where the code drifted from the plan, decide in the meeting which one changes, and change it in this PR.
3. Add a top banner: "Frozen at end of Week 2. Changes require a PR approved by all four members."
4. Add a CI check: a small test in `internal/store` that uses every method of `Store` so a signature change fails to compile, and the same in `internal/command` for `Handler` and `Ctx`.
5. Hold the freeze meeting: walk each contract, every member says "I can explain this".

## Acceptance

- [ ] PR approved by all four.
- [ ] MS0 exit criteria from T0.09 pass on all four machines.
- [ ] `docs/CONTRACTS.md` matches the code with no TODOs.
