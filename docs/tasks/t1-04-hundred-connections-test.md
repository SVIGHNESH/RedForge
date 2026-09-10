# T1.04 100-connection goroutine-count test

**Item:** M2 | **Milestone:** MS1 | **Week:** 4
**Suggested owner:** Vivek | **Reviewer:** Vighnesh
**Effort:** 2 hours
**Depends on:** T1.02, T1.03
**Unblocks:** M2 done

## Goal

Prove, in CI, that 100 open client connections do not create 100 goroutines.

## Steps

1. Add a `goroutines` field to the INFO `Server` section. Set it from `runtime.NumGoroutine()` when INFO is called. This is a debug field and is documented as such in the README.
2. Test: start server, read `goroutines` baseline with one connection, open 100 clients that each send `PING` and keep the socket open, read `goroutines` again through the first connection.
3. Assert the second reading equals the first. The count is the loop goroutine plus runtime goroutines plus the fsync goroutine once T5.02 lands, and none of those scale with clients.
4. Send `PING` on all 100 and assert all reply, to prove they are all served.

## Acceptance

- [ ] Test passes locally and in CI.
- [ ] The assertion fails if someone replaces the loop with `net.Listen` plus goroutine per connection (verify once by hand, then revert).
