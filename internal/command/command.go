package command

import (
	"strings"

	"github.com/SVIGHNESH/RedForge/internal/resp"
	"github.com/SVIGHNESH/RedForge/internal/server"
)

// Flags describe what a command does, so the dispatcher and later the log,
// the replica link and MULTI can reason about it without special-casing names.
type Flags uint8

const (
	// Write mutates the keyspace and may propagate to the log and replicas.
	Write Flags = 1 << iota
	// ReadOnly never mutates and never propagates.
	ReadOnly
	// Admin is a server management command.
	Admin
	// PubSub is allowed while a client is in subscribe mode.
	PubSub
	// NoTx may not be queued inside MULTI.
	NoTx
)

// Ctx is what a handler is given. The shape is frozen in MASTER-PLAN
// Section 4.4; the Store field arrives with T0.07.
type Ctx struct {
	Client *server.Client
	Srv    *server.Server
	// Now is the loop's view of the clock in milliseconds, read once per
	// command so every handler in one command sees the same instant.
	Now int64
}

// Propagate records the canonical form of a write for the log and for
// replicas. A handler calls it exactly once, and only when it actually
// changed something. T0.08 gives it the sequence number and T5.02 the log.
func (ctx *Ctx) Propagate(args [][]byte) {
	ctx.Srv.AppendPending(args)
}

// Handler is the frozen handler signature from MASTER-PLAN Section 4.4.
type Handler func(ctx *Ctx, args [][]byte) resp.Reply

// Command is one entry of the command matrix. Arity counts the command name
// itself; a negative arity means "at least this many".
type Command struct {
	Name    string
	Arity   int
	Flags   Flags
	Handler Handler
}

var table = map[string]*Command{}

// register adds a command to the table. Called from each file's init.
func register(cmd *Command) {
	table[strings.ToUpper(cmd.Name)] = cmd
}

// Lookup returns the command with this name, case-insensitively.
func Lookup(name string) (*Command, bool) {
	cmd, ok := table[strings.ToUpper(name)]
	return cmd, ok
}

// Names returns every registered command name. COMMAND and the tests use it.
func Names() []string {
	names := make([]string, 0, len(table))
	for name := range table {
		names = append(names, name)
	}
	return names
}
