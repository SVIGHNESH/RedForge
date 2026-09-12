package server

import (
	"errors"

	"github.com/SVIGHNESH/RedForge/internal/resp"
)

// maxOutputBuffer caps a client's pending output. A client that never reads
// its replies would otherwise let the server buffer without limit, so past
// this point the connection is dropped rather than the server.
const maxOutputBuffer = 64 * 1024 * 1024

// CommandFunc executes one parsed command and returns the reply to queue.
// T0.06 points this at the dispatcher; until then it answers +OK.
type CommandFunc func(s *Server, c *Client, args [][]byte) resp.Reply

// Command is the handler the loop calls for each complete frame.
var defaultCommand CommandFunc = func(_ *Server, _ *Client, _ [][]byte) resp.Reply { return resp.OK }

// SetCommand installs the command executor. Call it before Serve.
func (s *Server) SetCommand(fn CommandFunc) { s.command = fn }

// execute drains as many complete frames as c.rbuf holds, appending each reply
// to c.wbuf, and returns how many bytes it consumed. Everything the buffer
// holds is answered before a single flush, which is what makes pipelining a
// property of the loop rather than of any one command.
func execute(s *Server, c *Client) int {
	consumed := 0
	for {
		if len(c.wbuf) > maxOutputBuffer {
			// The peer is not draining its replies. Drop it instead of
			// growing without bound.
			c.closing = true
			return consumed
		}
		args, n, err := resp.ParseCommand(c.rbuf[consumed:])
		if err != nil {
			if errors.Is(err, resp.ErrIncomplete) {
				return consumed
			}
			// A protocol error is unrecoverable: the stream framing is lost,
			// so reply and close rather than trying to resynchronise.
			c.wbuf = resp.Error("ERR " + err.Error()).AppendTo(c.wbuf)
			c.closing = true
			return consumed + n
		}
		consumed += n
		if len(args) == 0 {
			// An empty inline line. Redis ignores it silently.
			continue
		}
		fn := s.command
		if fn == nil {
			fn = defaultCommand
		}
		c.wbuf = fn(s, c, args).AppendTo(c.wbuf)
		s.totalCommands.Add(1)
	}
}
