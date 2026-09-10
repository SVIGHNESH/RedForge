# T9.08 Replica side: apply loop and read-only enforcement (Stretch S1)

**Item:** S1 | **Milestone:** MS9 | **Week:** 10
**Suggested owner:** Ritk | **Reviewer:** Vighnesh
**Effort:** 3 hours
**Depends on:** T9.06
**Unblocks:** T9.09

## Goal

Records from the primary are applied through the normal dispatcher in replica mode, normal clients cannot write, and expiry on the replica works from the absolute timestamps.

## Steps

1. Apply path: `command.Dispatch` with a `FromPrimary` flag that disables replies, keeps the primary's `seq` (call `srv.SetSeq(seq)` after each record rather than `NextSeq`), and still appends to the replica's own log so a replica restart recovers locally and then re-syncs.
2. Read-only: in `Dispatch`, when `role == slave` and the command has the `Write` flag and the request is from a normal client, reply `-READONLY You can't write against a read only replica.`
3. Expiry: passive and active expiry run on the replica as on the primary. Since the primary does not propagate expiry deletions (T0.08 rule), the replica removes expired keys on its own from `ExpireAt`. Note the consequence: a replica can expire a key milliseconds before or after the primary. Write it in `DECISIONS.md`.
4. Tests: feed a record stream into a replica-mode server and assert the store; assert `SET` from a client gets `READONLY` while `GET` works; assert a key with a past `ExpireAt` received from the primary is not visible.

## Acceptance

- [ ] All three tests pass.
