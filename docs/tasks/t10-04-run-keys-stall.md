# T10.04 Run the KEYS stall measurement

**Item:** M10 | **Milestone:** MS10 | **Week:** 12
**Suggested owner:** Ritk | **Reviewer:** Rajat
**Effort:** 2 hours of machine time
**Depends on:** T8.05, T10.01
**Unblocks:** T10.06

## Steps

1. Check free memory is above 4 GB.
2. For N in 10000, 100000, 1000000, for target in rfs, redis: `bench/keys-stall.sh --target T --keys N`, three runs each.
3. On our server also collect `keys_last_iter_us` and `keys_last_reply_bytes` per run.
4. Fill `keys-stall.md`. Check that stall grows roughly linearly with N; if it does not, investigate before writing it up.

## Acceptance

- [ ] Table filled for both targets at all three sizes.
