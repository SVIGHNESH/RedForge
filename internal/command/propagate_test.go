package command

import (
	"testing"

	"github.com/SVIGHNESH/RedForge/internal/resp"
	"github.com/SVIGHNESH/RedForge/internal/server"
)

// record is one propagated write as seen by an Appender.
type record struct {
	seq  uint64
	args []string
}

// memoryAppender collects propagated records so command tests can assert the
// canonical form without any persistence code. Later tasks (T2.x, T3.x, T4.x,
// T5.02) reuse this shape to assert propagated forms.
type memoryAppender struct {
	recs []record
}

func (m *memoryAppender) Append(seq uint64, args [][]byte) error {
	cp := make([]string, len(args))
	for i, a := range args {
		cp[i] = string(a)
	}
	m.recs = append(m.recs, record{seq: seq, args: cp})
	return nil
}

// registerTestCommand adds a temporary command for one test and returns a
// cleanup func. It keeps TestOnlyTheDeclaredCommandsExist green.
func registerTestCommand(t *testing.T, cmd *Command) {
	t.Helper()
	name := cmd.Name
	if _, exists := table[name]; exists {
		t.Fatalf("test command %q already registered", name)
	}
	register(cmd)
	t.Cleanup(func() { delete(table, name) })
}

func dispatchArgs(names ...string) [][]byte {
	argv := make([][]byte, len(names))
	for i, s := range names {
		argv[i] = []byte(s)
	}
	return argv
}

// Two successful writes must get dense sequence numbers 1 and 2, and the
// appender must see the canonical form the handler propagated.
func TestPropagatedWritesGetDenseSequenceNumbers(t *testing.T) {
	ctx := newCtx()
	mem := &memoryAppender{}
	ctx.Srv.Appenders = []server.Appender{mem}
	registerTestCommand(t, &Command{
		Name: "TWRITE", Arity: -2, Flags: Write,
		Handler: func(ctx *Ctx, args [][]byte) resp.Reply {
			ctx.Propagate([]byte("TWRITE"), args[1])
			return resp.OK
		},
	})

	Dispatch(ctx, dispatchArgs("TWRITE", "a"))
	Dispatch(ctx, dispatchArgs("TWRITE", "b"))

	if got := ctx.Srv.CurrentSeq(); got != 2 {
		t.Fatalf("CurrentSeq() = %d, want 2", got)
	}
	if len(mem.recs) != 2 {
		t.Fatalf("appended %d records, want 2", len(mem.recs))
	}
	if mem.recs[0].seq != 1 || mem.recs[1].seq != 2 {
		t.Fatalf("seqs = %d,%d, want 1,2", mem.recs[0].seq, mem.recs[1].seq)
	}
	if mem.recs[0].args[1] != "a" || mem.recs[1].args[1] != "b" {
		t.Fatalf("propagated args = %v, want [TWRITE a] [TWRITE b]", mem.recs)
	}
}

// A handler that returns an error must not consume a sequence number and must
// not reach any appender, even if it called Propagate first.
func TestErrorReplyDoesNotConsumeSequence(t *testing.T) {
	ctx := newCtx()
	mem := &memoryAppender{}
	ctx.Srv.Appenders = []server.Appender{mem}
	registerTestCommand(t, &Command{
		Name: "TERR", Arity: 1, Flags: Write,
		Handler: func(ctx *Ctx, args [][]byte) resp.Reply {
			ctx.Propagate([]byte("TERR"))
			return resp.Error("ERR boom")
		},
	})

	reply := Dispatch(ctx, dispatchArgs("TERR"))
	if !resp.IsError(reply) {
		t.Fatalf("expected error reply, got %q", string(reply.AppendTo(nil)))
	}
	if got := ctx.Srv.CurrentSeq(); got != 0 {
		t.Fatalf("CurrentSeq() = %d, want 0 after error", got)
	}
	if len(mem.recs) != 0 {
		t.Fatalf("appender saw %v after error, want nothing", mem.recs)
	}
}

// A no-op write (handler returns OK without Propagate) must not advance the
// counter either, e.g. DEL on a missing key.
func TestNoPropagateMeansNoSequence(t *testing.T) {
	ctx := newCtx()
	mem := &memoryAppender{}
	ctx.Srv.Appenders = []server.Appender{mem}
	registerTestCommand(t, &Command{
		Name: "TNOP", Arity: 1, Flags: Write,
		Handler: func(ctx *Ctx, args [][]byte) resp.Reply {
			return resp.OK
		},
	})

	Dispatch(ctx, dispatchArgs("TNOP"))
	if got := ctx.Srv.CurrentSeq(); got != 0 {
		t.Fatalf("CurrentSeq() = %d, want 0 for no-op", got)
	}
	if len(mem.recs) != 0 {
		t.Fatalf("appender saw %v for no-op, want nothing", mem.recs)
	}
}

// A read-flagged command that strays into Propagate still propagates (so no
// write is silently lost) but the dispatcher logs a warning. The sequence
// still advances, which is what the test asserts; the warning goes to the log.
func TestReadFlaggedStrayPropagateStillAssignsSeq(t *testing.T) {
	ctx := newCtx()
	mem := &memoryAppender{}
	ctx.Srv.Appenders = []server.Appender{mem}
	registerTestCommand(t, &Command{
		Name: "TREADPROP", Arity: 1, Flags: ReadOnly,
		Handler: func(ctx *Ctx, args [][]byte) resp.Reply {
			ctx.Propagate([]byte("TREADPROP"))
			return resp.OK
		},
	})

	Dispatch(ctx, dispatchArgs("TREADPROP"))
	if got := ctx.Srv.CurrentSeq(); got != 1 {
		t.Fatalf("CurrentSeq() = %d, want 1", got)
	}
	if len(mem.recs) != 1 || mem.recs[0].seq != 1 {
		t.Fatalf("appender recs = %v, want one record at seq 1", mem.recs)
	}
}

// Calling Propagate twice in one command is a handler bug and must panic so
// it is caught in tests rather than silently logging twice.
func TestDoublePropagatePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on double Propagate")
		}
	}()
	ctx := newCtx()
	ctx.Propagate([]byte("A"))
	ctx.Propagate([]byte("B"))
}

// Propagate must copy the argument bytes: the loop reuses the read buffer, so
// retaining the caller's slice would corrupt the log.
func TestPropagateCopiesArgumentBytes(t *testing.T) {
	ctx := newCtx()
	mem := &memoryAppender{}
	ctx.Srv.Appenders = []server.Appender{mem}
	registerTestCommand(t, &Command{
		Name: "TCOPY", Arity: 2, Flags: Write,
		Handler: func(ctx *Ctx, args [][]byte) resp.Reply {
			ctx.Propagate([]byte("TCOPY"), args[1])
			return resp.OK
		},
	})

	argv := dispatchArgs("TCOPY", "v")
	Dispatch(ctx, argv)
	argv[1][0] = 'X'
	if mem.recs[0].args[1] != "v" {
		t.Fatalf("propagated arg = %q, want %q (was mutated after Dispatch)", mem.recs[0].args[1], "v")
	}
}

// SetSeq resumes the counter from recovery; the next propagated write gets
// exactly last+1, which keeps the log and snapshot unable to disagree.
func TestSetSeqResumesCounter(t *testing.T) {
	ctx := newCtx()
	mem := &memoryAppender{}
	ctx.Srv.Appenders = []server.Appender{mem}
	ctx.Srv.SetSeq(41)
	registerTestCommand(t, &Command{
		Name: "TRESUME", Arity: 1, Flags: Write,
		Handler: func(ctx *Ctx, args [][]byte) resp.Reply {
			ctx.Propagate([]byte("TRESUME"))
			return resp.OK
		},
	})

	Dispatch(ctx, dispatchArgs("TRESUME"))
	if got := ctx.Srv.CurrentSeq(); got != 42 {
		t.Fatalf("CurrentSeq() = %d, want 42", got)
	}
	if len(mem.recs) != 1 || mem.recs[0].seq != 42 {
		t.Fatalf("appender recs = %v, want seq 42", mem.recs)
	}
}
