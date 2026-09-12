package command

import "github.com/SVIGHNESH/RedForge/internal/resp"

func init() {
	register(&Command{Name: "PING", Arity: -1, Flags: ReadOnly | PubSub, Handler: pingCommand})
	register(&Command{Name: "ECHO", Arity: 2, Flags: ReadOnly, Handler: echoCommand})
	register(&Command{Name: "QUIT", Arity: 1, Flags: ReadOnly | NoTx, Handler: quitCommand})
}

// PING with no argument answers +PONG; with one argument it echoes it back as
// a bulk string, which is what redis-cli --latency and clients rely on.
func pingCommand(_ *Ctx, args [][]byte) resp.Reply {
	switch len(args) {
	case 1:
		return resp.PONG
	case 2:
		return resp.Bulk(args[1])
	default:
		return resp.Error("ERR wrong number of arguments for 'ping' command")
	}
}

func echoCommand(_ *Ctx, args [][]byte) resp.Reply {
	return resp.Bulk(args[1])
}

// QUIT replies +OK and marks the client closed. The loop closes it only once
// that reply has drained.
func quitCommand(ctx *Ctx, _ [][]byte) resp.Reply {
	ctx.Client.Close()
	return resp.OK
}
