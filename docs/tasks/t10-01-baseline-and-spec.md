# T10.01 Install the Redis baseline and capture the spec

**Item:** M10 | **Milestone:** MS10 | **Week:** 12, day 1
**Suggested owner:** Ritk | **Reviewer:** Rajat
**Effort:** 1 to 2 hours
**Depends on:** T8.03
**Unblocks:** T10.02 to T10.04

## Goal

The evaluation machine is set up and its state recorded before any number is taken.

## Steps

1. Pick the machine. It must be one machine for both servers, per synopsis Section 7.2. Prefer a machine with at least 4 cores and 16 GB (Table 6.1).
2. Install official Redis 7.x (`pacman -S redis` or `apt install redis-server`), disable the system service so no stray instance runs, verify `redis-server --version`.
3. Set the CPU governor to `performance`, close browsers and other load, disable swap if possible.
4. Build our server from the tagged evaluation commit: `git tag eval-2026-12` and `make build`.
5. Run `bench/spec.sh` into `bench/results/<date>/spec.txt` and commit it.
6. Sanity run of Scenario 1 on both targets once each and confirm the CSV parses with the report tool.

## Acceptance

- [ ] `spec.txt` committed with governor `performance`.
- [ ] Both sanity runs produce parseable output.
