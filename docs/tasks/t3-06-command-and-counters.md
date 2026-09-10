# T3.06 COMMAND and stats counters

**Item:** M11 | **Milestone:** MS3 | **Week:** 7
**Suggested owner:** Vighnesh | **Reviewer:** Rajat
**Effort:** 1 to 2 hours
**Depends on:** T0.06
**Unblocks:** M11 partial

## Goal

`COMMAND` and `COMMAND COUNT` report the real command table so tooling that introspects the server gets truthful data.
Other subcommands, including `DOCS`, return an error that `redis-cli` tolerates.

## Steps

1. Register `COMMAND` (-1, ReadOnly).
2. No subcommand: array with one entry per registered command in the Redis 7 shape: name, arity, flags array (`write` or `readonly`, plus `admin`, `pubsub` where set), first key 1, last key 1, step 1, an empty ACL categories array. Keep first/last/step at 1/1/1 for multi-key commands; precision here is not needed within scope.
3. `COUNT`: integer.
4. Anything else: `-ERR unknown subcommand 'DOCS'. Try COMMAND HELP.`
5. Test that `redis-cli` interactive still opens (T0.09 test already covers it) and that `COMMAND COUNT` equals `len(table)`.

## Acceptance

- [ ] `redis-cli COMMAND COUNT` prints the number of registered commands.
- [ ] `redis-cli COMMAND` prints the array without protocol errors.
