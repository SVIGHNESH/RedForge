package command

import (
	"log"
	"strings"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/resp"
	"github.com/SVIGHNESH/RedForge/internal/server"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

// maxUnknownArgs is how many arguments Redis quotes back in the unknown
// command error before it stops.
const maxUnknownArgs = 3

// Dispatch runs one command. Unknown names and arity mismatches are answered
// with Redis's exact wording (MASTER-PLAN Section 4.11) so redis-cli and the
// integration tests behave identically against this server and against Redis.
//
// When the handler records a write with Ctx.Propagate and returns a non-error
// reply, Dispatch assigns the next sequence number (MASTER-PLAN Section 4.5)
// and fans the canonical form out to every registered Appender. Error replies
// never consume a sequence number. A read-flagged command that propagated is
// a bug: the write still propagates, but a warning is logged.
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
	reply := cmd.Handler(ctx, args)
	pending := ctx.pending
	ctx.pending = nil
	if pending == nil || resp.IsError(reply) {
		return reply
	}
	if cmd.Flags&Write == 0 {
		log.Printf("command: %s propagated a write but is not flagged Write", cmd.Name)
	}
	seq := ctx.Srv.NextSeq()
	for _, a := range ctx.Srv.Appenders {
		if err := a.Append(seq, pending); err != nil {
			log.Printf("command: appender failed at seq %d: %v", seq, err)
		}
	}
	return reply
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

// Active expiry parameters from MASTER-PLAN Section 4 (P3.2): sample 20 keys
// per round and spend at most 1 ms of loop time per tick.
const (
	sweepSample = 20
	sweepBudget = time.Millisecond
)

// Install points the event loop at this dispatcher. It builds one Ctx per
// command over the shared keyspace st, with Now read once from clock on the
// loop thread so every step of one command sees the same instant. It also
// registers the active expiry sweep on the loop tick, so expired keys that
// are never touched again are reclaimed without any extra goroutine.
func Install(s *server.Server, st store.Store, clock store.Clock) {
	s.SetCommand(func(srv *server.Server, c *server.Client, args [][]byte) resp.Reply {
		ctx := &Ctx{Client: c, Store: st, Srv: srv, Now: clock.NowMs()}
		return Dispatch(ctx, args)
	})
	s.OnTick(func() { st.SweepExpired(sweepSample, sweepBudget) })
}
