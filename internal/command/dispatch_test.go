package command

import (
	"strings"
	"testing"

	"github.com/SVIGHNESH/RedForge/internal/server"
)

func reply(t *testing.T, ctx *Ctx, args ...string) string {
	t.Helper()
	argv := make([][]byte, len(args))
	for i, a := range args {
		argv[i] = []byte(a)
	}
	return string(Dispatch(ctx, argv).AppendTo(nil))
}

func newCtx() *Ctx {
	srv := server.New()
	return &Ctx{Client: &server.Client{}, Srv: srv, Now: 1700000000000}
}

func TestPingEchoQuit(t *testing.T) {
	ctx := newCtx()
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"PING"}, "+PONG\r\n"},
		{[]string{"ping"}, "+PONG\r\n"},
		{[]string{"PING", "hello"}, "$5\r\nhello\r\n"},
		{[]string{"ECHO", "hi"}, "$2\r\nhi\r\n"},
		{[]string{"echo", ""}, "$0\r\n\r\n"},
		{[]string{"QUIT"}, "+OK\r\n"},
	}
	for _, tc := range cases {
		if got := reply(t, ctx, tc.args...); got != tc.want {
			t.Errorf("%v = %q, want %q", tc.args, got, tc.want)
		}
	}
}

// QUIT must not close the socket itself: the loop closes it after the +OK has
// drained, otherwise redis-cli reports a broken connection on exit.
func TestQuitMarksClosingWithoutDroppingTheReply(t *testing.T) {
	ctx := newCtx()
	got := reply(t, ctx, "QUIT")
	if got != "+OK\r\n" {
		t.Fatalf("QUIT = %q, want +OK", got)
	}
	if !ctx.Client.Closing() {
		t.Fatal("QUIT did not mark the client closing")
	}
}

// The wording is byte-for-byte what redis-server 7 emits, trailing space and
// all, because the integration tests and redis-cli read these strings.
func TestUnknownCommandWording(t *testing.T) {
	ctx := newCtx()
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"FOO", "a", "b"}, "-ERR unknown command 'FOO', with args beginning with: 'a' 'b' \r\n"},
		{[]string{"FOO"}, "-ERR unknown command 'FOO', with args beginning with: \r\n"},
		{[]string{"FOO", "a", "b", "c", "d"}, "-ERR unknown command 'FOO', with args beginning with: 'a' 'b' 'c' \r\n"},
		{[]string{"HELLO"}, "-ERR unknown command 'HELLO', with args beginning with: \r\n"},
	}
	for _, tc := range cases {
		if got := reply(t, ctx, tc.args...); got != tc.want {
			t.Errorf("%v =\n %q\nwant\n %q", tc.args, got, tc.want)
		}
	}
}

// HELLO is deliberately absent: the matrix in MASTER-PLAN Section 1.2 is the
// whole command set, and a stray registration is a scope violation.
func TestOnlyTheDeclaredCommandsExist(t *testing.T) {
	want := map[string]bool{"PING": true, "ECHO": true, "QUIT": true}
	for _, name := range Names() {
		if !want[name] {
			t.Errorf("unexpected command registered: %q", name)
		}
		delete(want, name)
	}
	for name := range want {
		t.Errorf("missing command: %q", name)
	}
}

func TestArityErrors(t *testing.T) {
	ctx := newCtx()
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"ECHO"}, "-ERR wrong number of arguments for 'echo' command\r\n"},
		{[]string{"ECHO", "a", "b"}, "-ERR wrong number of arguments for 'echo' command\r\n"},
		{[]string{"QUIT", "now"}, "-ERR wrong number of arguments for 'quit' command\r\n"},
		{[]string{"PING", "a", "b"}, "-ERR wrong number of arguments for 'ping' command\r\n"},
	}
	for _, tc := range cases {
		if got := reply(t, ctx, tc.args...); got != tc.want {
			t.Errorf("%v = %q, want %q", tc.args, got, tc.want)
		}
	}
}

// A reply must never be able to carry an injected second frame, whatever the
// client puts in an argument.
func TestErrorRepliesCannotInjectAFrame(t *testing.T) {
	ctx := newCtx()
	got := reply(t, ctx, "FOO", "a\r\n+INJECTED")
	if strings.Count(got, "\r\n") != 1 {
		t.Fatalf("reply %q contains more than one frame terminator", got)
	}
}

// Reads must not propagate. Nothing here writes, so the sequence counter must
// stay at zero even with an appender registered.
func TestReadsDoNotPropagate(t *testing.T) {
	ctx := newCtx()
	mem := &memoryAppender{}
	ctx.Srv.Appenders = []server.Appender{mem}
	for _, args := range [][]string{{"PING"}, {"ECHO", "x"}, {"QUIT"}, {"FOO"}} {
		reply(t, ctx, args...)
	}
	if got := ctx.Srv.CurrentSeq(); got != 0 {
		t.Fatalf("CurrentSeq() = %d, want 0 after reads", got)
	}
	if len(mem.recs) != 0 {
		t.Fatalf("appender saw %v after reads, want nothing", mem.recs)
	}
}

func TestLookupIsCaseInsensitive(t *testing.T) {
	for _, name := range []string{"ping", "PING", "PiNg"} {
		if _, ok := Lookup(name); !ok {
			t.Errorf("Lookup(%q) missed", name)
		}
	}
	if _, ok := Lookup("HELLO"); ok {
		t.Error("HELLO must not be registered")
	}
}
