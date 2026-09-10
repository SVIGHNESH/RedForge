# T1.02 Test harness: ServerProcess

**Item:** M9 | **Milestone:** MS1 | **Week:** 3
**Suggested owner:** Vivek | **Reviewer:** Rajat
**Effort:** 3 hours
**Depends on:** T0.01
**Unblocks:** every integration, crash and expiry test

## Goal

`internal/testutil` can build the server once per test run, start it on a free port with a temp data directory, kill it with SIGKILL, and restart it on the same directory.
Every crash-recovery test uses this and nothing else.

## Steps

1. `func BuildOnce(t) string` compiles `cmd/redis-from-scratch` into `t.TempDir()` guarded by `sync.Once`, returning the binary path.
2. `type ServerProcess struct { Port int; Dir string; cmd *exec.Cmd; ... }` with `Start(t, flags ...string)`, `Kill()` (sends `SIGKILL` and waits), `Stop()` (SIGTERM, then kill after 2 s), `Restart(t)` (Kill then Start with the same flags and dir), `WaitReady(t)` (dials until `PING` answers, 5 s limit).
3. Free port: bind `:0`, read the port, close, use it. Accept the small race.
4. Capture stdout and stderr to a buffer and print them with `t.Log` on failure.
5. `t.Cleanup` kills the process.
6. Provide `Info(t, section string) map[string]string` that runs `INFO` and parses `key:value` lines, because many tests poll INFO.
7. `WaitFor(t, func() bool, timeout)` polling helper with 10 ms interval.

## Acceptance

- [ ] A test that starts, kills, restarts and pings passes in under 2 s.
- [ ] Two servers can run in one test on different ports and dirs.
