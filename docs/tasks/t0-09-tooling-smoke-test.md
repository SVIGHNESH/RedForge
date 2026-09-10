# T0.09 Client tooling smoke test

**Item:** M1 | **Milestone:** MS0 | **Week:** 2
**Suggested owner:** anyone with redis-tools installed | **Reviewer:** Vivek
**Effort:** 1 to 2 hours
**Depends on:** T0.06
**Unblocks:** MS0 exit gate

## Goal

Prove the four MS0 exit criteria and lock them into an automated test so they can never regress.
This catches the two known client behaviours: `redis-cli` 7.x sends `COMMAND DOCS` on connect and `redis-benchmark` 7.x sends `CONFIG GET`.
Both must receive an error reply and carry on.

## Context you need

- Install `redis-tools` (Arch: `redis` package; Ubuntu: `redis-tools`). Record `redis-cli --version` in the PR.
- The server returns `-ERR unknown command` for anything not in the table, which is what both tools tolerate.

## Steps

1. Start `./bin/redis-from-scratch --port 6380`.
2. Run and paste output into the PR: `redis-cli -p 6380 PING`; `redis-cli -p 6380` interactive then `PING` then `quit`; `redis-benchmark -p 6380 -t ping -n 10000 -q`; `printf 'PING\r\nPING\r\n' | nc localhost 6380`.
3. Write `test/integration/tooling_test.go` that skips if `redis-cli` is not on `PATH`, starts the binary on a free port, and runs the first, third and fourth checks through `os/exec`, asserting exact output.
4. If `redis-cli` interactive hangs, capture the bytes it sends with `strace -f -e trace=write redis-cli -p 6380` and handle the case. Do not add the command it sends; make sure the error path works.

## Acceptance

- [ ] All four checks produce the expected output on every member's machine (each member comments on the PR with their output).
- [ ] The integration test passes in CI.
