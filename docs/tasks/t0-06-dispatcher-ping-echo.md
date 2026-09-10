# T0.06 Command table, dispatcher, PING, ECHO, QUIT

**Item:** M1 | **Milestone:** MS0 | **Week:** 2
**Suggested owner:** anyone | **Reviewer:** Vighnesh
**Effort:** 2 to 3 hours
**Depends on:** T0.03, T0.05
**Unblocks:** every command task

## Goal

`internal/command` owns the table of supported commands and a `Dispatch` function the loop calls per parsed command.
`PING`, `ECHO` and `QUIT` work end to end through `redis-cli`.

## Context you need

Frozen signature from MASTER-PLAN Section 4.4:

```go
type Handler func(ctx *Ctx, args [][]byte) resp.Reply
type Ctx struct { Client *server.Client; Store store.Store; Now int64; Srv *server.Server }
type Command struct { Name string; Arity int; Flags Flags; Handler Handler }
```

`Arity` counts the command name. Negative means "at least that many".
`PING` is `-1`, `ECHO` is `2`, `QUIT` is `1`.

Redis error wording, used verbatim:

```
ERR unknown command 'foo', with args beginning with: 'a' 'b'
ERR wrong number of arguments for 'echo' command
```

## Steps

1. Define `Flags` bits: `Write`, `ReadOnly`, `Admin`, `PubSub`, `NoTx`.
2. `var table = map[string]*Command{}` populated in `init()` from per-file `register(cmd)` calls. Lookup is case-insensitive: uppercase `args[0]` once.
3. `Dispatch(ctx, args)`: unknown name returns the unknown-command error with up to the first three args quoted; arity mismatch returns the wrong-arguments error; otherwise call the handler. Increment `Srv.TotalCommands`.
4. `PING` with no argument returns `+PONG`, with one argument returns it as a bulk string. `ECHO` returns its argument as a bulk string. `QUIT` returns `+OK` and marks the client `closing`.
5. Wire the loop's execute stub to `Dispatch`, building `Ctx` once per command with `Now` read from the clock.
6. `ctx.Propagate` exists as a method that appends `args` to `Srv.pendingLog [][]byte` for now. T0.08 turns this into the sequence counter and T5.02 into the real log.
7. Unit tests for dispatch errors and for the three commands.

## Acceptance

- [ ] `redis-cli -p 6380 PING` prints `PONG`; `redis-cli -p 6380 ECHO hi` prints `"hi"`.
- [ ] `redis-cli -p 6380 FOO a b` prints the unknown-command error with the exact Redis wording.
- [ ] `redis-cli -p 6380` interactive mode opens and exits cleanly with `quit`.
