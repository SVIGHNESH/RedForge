# T9.09 Replication topology: Compose, end-to-end test, diagram (Stretch S1)

**Item:** S1, M11 | **Milestone:** MS9 | **Week:** 11
**Suggested owner:** Vivek | **Reviewer:** Rajat
**Effort:** 3 hours
**Depends on:** T9.06, T9.07, T9.08, T11.01
**Unblocks:** S1 done

## Goal

The synopsis success criterion for Objective 6: a write on the primary is visible on two replicas and a write to a replica is rejected.

## Steps

1. `deploy/docker-compose.yml` gains services `primary`, `replica1`, `replica2`, the replicas started with `--replicaof primary 6380`.
2. `test/integration/replication_test.go`: start three `testutil.ServerProcess` instances locally (no Docker in CI), replicas pointed at the primary; `SET k v` on primary; `WaitFor` `GET k` equals `v` on both replicas within 2 s; `SET` on a replica returns `READONLY`; `LPUSH`, `HSET`, `SADD`, `EXPIRE` all replicate; `Kill()` replica1, write more, `Restart()` replica1, assert it catches up via full sync.
3. `docs/diagrams/replication.dot`: handshake, snapshot transfer, steady-state stream, with the state names from T9.06.
4. Add to the demo script: write on primary, read on both, rejected write on replica.

## Acceptance

- [ ] Test passes in CI.
- [ ] `docker compose up` shows all three logs and the manual demo works.
