# T1.05 One-command test runner and CI with redis-tools

**Item:** M9 | **Milestone:** MS1 | **Week:** 4
**Suggested owner:** Vivek | **Reviewer:** Rajat
**Effort:** 1 to 2 hours
**Depends on:** T0.01, T1.02
**Unblocks:** M9 done

## Goal

`make test` from a clean checkout runs unit tests and integration tests, builds the binary the integration tests need, and CI runs exactly the same target with `redis-cli` available.

## Steps

1. `make test` runs `go build -o bin/redis-from-scratch ./cmd/redis-from-scratch` then `go test -race -count=1 ./...`. Integration tests use `testutil.BuildOnce`, so they do not depend on `bin/`, but building first surfaces compile errors faster.
2. Integration tests live under `test/integration` with no build tag, so a plain `go test ./...` runs them. Tests that need `redis-cli` call `t.Skip` when it is not on `PATH`, and CI installs it so nothing is skipped there.
3. Add `make test-short` that passes `-short`, and have slow tests (anything sleeping more than 2 s) check `testing.Short()`.
4. CI: cache the Go build, run `make lint` then `make test`, upload server logs from failed tests as an artifact.
5. Add a job timeout of 15 minutes.

## Acceptance

- [ ] A fresh `git clone` followed by `make test` passes with nothing else installed except Go and redis-tools.
- [ ] CI shows zero skipped tests.
