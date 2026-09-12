package command

import (
	"strings"

	"github.com/SVIGHNESH/RedForge/internal/resp"
	"github.com/SVIGHNESH/RedForge/internal/server"
)

// maxUnknownArgs is how many arguments Redis quotes back in the unknown
// command error before it stops.
const maxUnknownArgs = 3

// Dispatch runs one command. Unknown names and arity mismatches are answered
// with Redis's exact wording (MASTER-PLAN Section 4.11) so redis-cli and the
// integration tests behave identically against this server and against Redis.
func Dispatch(ctx *Ctx, args [][]byte) resp.Reply {
	if len(args) == 0 {
		return resp.OK
	}
	name := strings.ToUpper(string(args[0]))
	cmd, ok := table[name]
	if !ok {
		return resp.Error(unknownCommandError(args))
	}
	if !arityOK(cmd.Arity, len(args)) {
		return resp.Error("ERR wrong number of arguments for '" + strings.ToLower(name) + "' command")
	}
	return cmd.Handler(ctx, args)
}

func arityOK(arity, got int) bool {
	if arity >= 0 {
		return got == arity
	}
	return got >= -arity
}

// unknownCommandError renders what redis-server 7 renders, byte for byte,
// including the trailing space after the last argument:
//
//	ERR unknown command 'FOO', with args beginning with: 'a' 'b'
//
// The command name is echoed as the client sent it, not upper-cased.
func unknownCommandError(args [][]byte) string {
	var b strings.Builder
	b.WriteString("ERR unknown command '")
	b.Write(sanitise(args[0]))
	b.WriteString("', with args beginning with: ")
	for i := 1; i < len(args) && i <= maxUnknownArgs; i++ {
		b.WriteByte('\'')
		b.Write(sanitise(args[i]))
		b.WriteString("' ")
	}
	return b.String()
}

// sanitise strips CR and LF so a hostile argument cannot inject a second
// reply into the stream through an error message.
func sanitise(arg []byte) []byte {
	out := make([]byte, 0, len(arg))
	for _, ch := range arg {
		if ch == '\r' || ch == '\n' {
			continue
		}
		out = append(out, ch)
	}
	return out
}

// Install points the event loop at this dispatcher. It builds one Ctx per
// command, with Now read on the loop thread.
func Install(s *server.Server, now func() int64) {
	s.SetCommand(func(srv *server.Server, c *server.Client, args [][]byte) resp.Reply {
		ctx := &Ctx{Client: c, Srv: srv, Now: now()}
		return Dispatch(ctx, args)
	})
}
