# T0.01 Repository skeleton, Makefile and CI

**Item:** M9, M11 | **Milestone:** MS0 | **Week:** 1
**Suggested owner:** anyone | **Reviewer:** Vighnesh
**Effort:** 2 to 3 hours
**Depends on:** nothing
**Unblocks:** every other task

## Goal

A clone of the repo builds, tests and lints with one command each, locally and on GitHub Actions.

## Context you need

- Module path is `github.com/<org>/redis-from-scratch`. Ask Vighnesh for the org name.
- `go.mod` declares `go 1.22` and must never gain a `require` block. Standard library only.
- Directory layout from MASTER-PLAN Section 4.1. Create the directories now with a `doc.go` in each so the tree exists.

## Steps

1. `go mod init github.com/<org>/redis-from-scratch`.
2. Create `cmd/redis-from-scratch/main.go` that prints the version and exits. Version constant lives in `internal/server/version.go` as `0.1.0-rfs`.
3. Create `internal/resp`, `internal/server`, `internal/command`, `internal/store`, `internal/aof`, `internal/snapshot`, `internal/recovery`, `internal/testutil`, `test/integration`, `bench`, `deploy`, `docs/diagrams`, each with a `doc.go` or `.gitkeep`.
4. Write `Makefile` with targets: `build` (outputs `bin/redis-from-scratch`), `test` (`go test ./...`), `lint` (`gofmt -l .` fails if non-empty, then `go vet ./...`), `bench` (placeholder that prints "see bench/").
5. Write `.github/workflows/ci.yml`: on pull request and push to main, `ubuntu-latest`, `actions/setup-go` with `1.22`, `apt-get install -y redis-tools`, then `make lint` and `make test`.
6. Add `.gitignore` for `bin/`, `data/`, `*.aof`, `*.snap`, `bench/results/`.
7. Add `.github/pull_request_template.md` containing the checklist from MASTER-PLAN Section 1.3.
8. Push, open a PR, confirm CI is green.

## Acceptance

- [ ] `make build && ./bin/redis-from-scratch --version` prints `0.1.0-rfs`.
- [ ] `make lint` and `make test` pass on a fresh clone.
- [ ] CI is green on the PR and the PR template appears.
- [ ] `go.mod` has no `require`.
