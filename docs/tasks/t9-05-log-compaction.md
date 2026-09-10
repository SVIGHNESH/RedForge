# T9.05 Log compaction after BGSAVE (Stretch S3)

**Item:** S3 | **Milestone:** MS9 | **Week:** 8
**Suggested owner:** Rajat | **Reviewer:** Ritk
**Effort:** 4 hours
**Depends on:** T6.03, T6.04; all of Rajat's Must items merged
**Unblocks:** S3 done

## Goal

After a successful snapshot with boundary N, the log is rewritten to contain only records with `seq > N`, while writes continue, and recovery is unaffected at every instant.

## Context you need

MASTER-PLAN Section 4.8, last paragraph. Sequence:

1. Snapshot rename succeeds with boundary N (loop learns this via the eventfd).
2. Loop starts a copier goroutine: read `appendonly.aof`, write records with `seq > N` into `appendonly.aof.new`, then signal completion with the offset up to which it copied.
3. Loop, on completion: flush its writer, copy any records appended after that offset into the new file synchronously (small), `Sync`, `Rename` new over old, swap the writer's descriptor, `Sync` the directory.
4. Any crash before the rename leaves the old file intact; replay skips `seq <= N` anyway.

## Steps

1. Implement the copier in `internal/aof/compact.go` using the `Reader` and the eventfd registration from T6.03.
2. Guard: only one compaction at a time; a `BGSAVE` completing during a compaction defers its compaction until the current one finishes.
3. INFO: `aof_last_compaction_status`, `aof_size_bytes`.
4. Test: write 10000 keys, `BGSAVE`, keep writing 1000 more during the save and compaction, wait for `aof_last_compaction_status:ok`, assert the file shrank, `Kill()`, `Restart()`, verify all 11000; repeat with a kill in the middle of compaction (add `--debug-compact-delay-ms`).

## Acceptance

- [ ] Both tests pass.
- [ ] File size after compaction is close to the size of the post-snapshot writes only.
