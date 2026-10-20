package command

import (
	"sort"
	"strings"

	"github.com/SVIGHNESH/RedForge/internal/resp"
)

func init() {
	register(&Command{Name: "COMMAND", Arity: -1, Flags: ReadOnly, Handler: commandCommand})
}

// keySpec is a command's first key, last key and key step, the three
// positions Redis reports in COMMAND. Most commands take one key at
// position 1; the exceptions are listed in keySpecs.
type keySpec struct{ first, last, step int64 }

var (
	oneKey   = keySpec{1, 1, 1}
	noKeys   = keySpec{0, 0, 0}
	allKeys  = keySpec{1, -1, 1}
	keySpecs = map[string]keySpec{
		"PING": noKeys, "ECHO": noKeys, "QUIT": noKeys, "INFO": noKeys,
		"COMMAND": noKeys, "BGSAVE": noKeys, "MULTI": noKeys, "EXEC": noKeys,
		"DISCARD": noKeys, "SUBSCRIBE": noKeys, "UNSUBSCRIBE": noKeys,
		"PUBLISH": noKeys, "REPLICAOF": noKeys,
		"DEL": allKeys, "EXISTS": allKeys, "SINTER": allKeys,
	}
)

// COMMAND reports the real command table so tools that introspect the
// server get truthful data (T3.06):
//
//	COMMAND        one entry per registered command, Redis shape
//	COMMAND COUNT  the number of registered commands
//	anything else  an error, which redis-cli tolerates; redis-cli 7 sends
//	               COMMAND DOCS on connect and carries on without hints
//	               (MASTER-PLAN Section 2 item 5)
func commandCommand(_ *Ctx, args [][]byte) resp.Reply {
	if len(args) == 1 {
		names := Names()
		sort.Strings(names)
		entries := make([]resp.Reply, len(names))
		for i, name := range names {
			entries[i] = commandEntry(table[name])
		}
		return resp.Array(entries...)
	}
	sub := string(args[1])
	if strings.EqualFold(sub, "COUNT") {
		if len(args) != 2 {
			return resp.Error("ERR wrong number of arguments for 'command|count' command")
		}
		return resp.Integer(int64(len(table)))
	}
	return resp.Error("ERR unknown subcommand '" + string(sanitise(args[1])) + "'. Try COMMAND HELP.")
}

// commandEntry is one command in the Redis 6 COMMAND shape: name, arity,
// flags, first key, last key, key step, ACL categories (always empty here).
func commandEntry(cmd *Command) resp.Reply {
	spec, ok := keySpecs[cmd.Name]
	if !ok {
		spec = oneKey
	}
	return resp.Array(
		resp.BulkString(strings.ToLower(cmd.Name)),
		resp.Integer(int64(cmd.Arity)),
		resp.Array(flagReplies(cmd.Flags)...),
		resp.Integer(spec.first),
		resp.Integer(spec.last),
		resp.Integer(spec.step),
		resp.EmptyArray(),
	)
}

// flagReplies names the flags Redis clients understand. NoTx is internal to
// MULTI and has no Redis counterpart, so it is not reported.
func flagReplies(f Flags) []resp.Reply {
	var out []resp.Reply
	if f&Write != 0 {
		out = append(out, resp.SimpleString("write"))
	}
	if f&ReadOnly != 0 {
		out = append(out, resp.SimpleString("readonly"))
	}
	if f&Admin != 0 {
		out = append(out, resp.SimpleString("admin"))
	}
	if f&PubSub != 0 {
		out = append(out, resp.SimpleString("pubsub"))
	}
	return out
}
