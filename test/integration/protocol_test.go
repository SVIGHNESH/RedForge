package integration

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/command"
	"github.com/SVIGHNESH/RedForge/internal/server"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

// startServer runs the real loop and dispatcher on an ephemeral port inside
// the test process. The ServerProcess harness (T1.02) will run the binary
// instead; these protocol cases only need the loop and the dispatcher.
func startServer(t *testing.T) string {
	t.Helper()
	srv := server.New()
	clock := store.RealClock{}
	command.Install(srv, store.New(clock), clock)
	if err := srv.Listen("127.0.0.1", 0); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	port, err := srv.Port()
	if err != nil {
		t.Fatalf("Port: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve() }()
	t.Cleanup(func() {
		srv.Stop()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Serve: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("Serve did not return within 2s of Stop")
		}
		srv.Close()
	})
	return fmt.Sprintf("127.0.0.1:%d", port)
}

func dial(t *testing.T, addr string) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func send(t *testing.T, conn net.Conn, b string) {
	t.Helper()
	if _, err := conn.Write([]byte(b)); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func readExactly(t *testing.T, conn net.Conn, want string) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	got := make([]byte, len(want))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read %q: %v (got %q)", want, err, got)
	}
	if string(got) != want {
		t.Fatalf("reply = %q, want %q", got, want)
	}
}

// assertPing proves a connection is still open and in sync.
func assertPing(t *testing.T, conn net.Conn) {
	t.Helper()
	send(t, conn, "*1\r\n$4\r\nPING\r\n")
	readExactly(t, conn, "+PONG\r\n")
}

// Each malformed request must earn exactly the reply redis-server sends for
// the same bytes (captured from Valkey 9.1, Redis 7 wording), and then the
// server must close the socket within 100 ms of the reply.
func TestProtocolErrorsReplyThenClose(t *testing.T) {
	addr := startServer(t)
	cases := []struct {
		name, in, want string
	}{
		{"null bulk argument", "*2\r\n$4\r\nECHO\r\n$-1\r\n", "invalid bulk length"},
		{"plus in bulk length", "*1\r\n$+4\r\nPING\r\n", "invalid bulk length"},
		{"non-digit bulk length", "*1\r\n$x\r\n", "invalid bulk length"},
		{"array above the cap", "*1048577\r\n", "invalid multibulk length"},
		{"plus in array length", "*+1\r\n$4\r\nPING\r\n", "invalid multibulk length"},
		{"non-bulk element", "*1\r\n:1\r\n", "expected '$', got ':'"},
		{"lf without cr", "*1\n", "unbalanced line ending"},
		{"inline over 64 KB", strings.Repeat("a", 64*1024+1), "too big inline request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := dial(t, addr)
			send(t, conn, tc.in)
			readExactly(t, conn, "-ERR Protocol error: "+tc.want+"\r\n")

			// The next read must be EOF, and quickly: the server closes as
			// soon as the error reply has drained.
			start := time.Now()
			conn.SetReadDeadline(start.Add(time.Second))
			n, err := conn.Read(make([]byte, 1))
			if n != 0 || !errors.Is(err, io.EOF) {
				t.Fatalf("after protocol error read n=%d err=%v, want EOF", n, err)
			}
			if d := time.Since(start); d > 100*time.Millisecond {
				t.Fatalf("socket closed after %v, want within 100ms", d)
			}
		})
	}
}

// Bytes pipelined after a bad frame are never executed: the reply stream holds
// exactly one error and then ends.
func TestNothingRunsAfterAProtocolError(t *testing.T) {
	conn := dial(t, startServer(t))
	send(t, conn, "*1\r\n:1\r\n*1\r\n$4\r\nPING\r\nPING\r\n")
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	got, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if want := "-ERR Protocol error: expected '$', got ':'\r\n"; string(got) != want {
		t.Fatalf("stream = %q, want only %q", got, want)
	}
}

// Empty requests are ignored without a reply and the connection stays usable,
// as in Redis.
func TestEmptyRequestsAreIgnored(t *testing.T) {
	conn := dial(t, startServer(t))
	send(t, conn, "*0\r\n*-1\r\n\r\n")
	assertPing(t, conn)
}

// HELLO is not in the command matrix, so RESP3 negotiation fails with the
// ordinary unknown-command error and the client stays on RESP2.
func TestHelloIsRejected(t *testing.T) {
	conn := dial(t, startServer(t))
	send(t, conn, "*2\r\n$5\r\nHELLO\r\n$1\r\n3\r\n")
	readExactly(t, conn, "-ERR unknown command 'HELLO', with args beginning with: '3' \r\n")
	assertPing(t, conn)
}

// A non-'*' frame is an inline command in Redis, so a bare "+OK" is an unknown
// command, not a protocol error, and the connection survives.
func TestBareNonArrayFrameIsInline(t *testing.T) {
	conn := dial(t, startServer(t))
	send(t, conn, "+OK\r\n")
	readExactly(t, conn, "-ERR unknown command '+OK', with args beginning with: \r\n")
	assertPing(t, conn)
}

// A command trickled in one byte at a time must yield exactly one reply.
func TestCommandSentOneByteAtATime(t *testing.T) {
	conn := dial(t, startServer(t))
	for _, b := range []byte("*2\r\n$4\r\nECHO\r\n$5\r\nhello\r\n") {
		send(t, conn, string(b))
		time.Sleep(time.Millisecond)
	}
	readExactly(t, conn, "$5\r\nhello\r\n")

	// Nothing else may follow that single reply.
	conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	if n, _ := conn.Read(make([]byte, 1)); n != 0 {
		t.Fatal("got bytes after the only reply")
	}
}

// One bad client must not stall anyone else: while a client holds half a frame
// open and another trips a protocol error, a third connection keeps working.
func TestBadClientsDoNotStallOthers(t *testing.T) {
	addr := startServer(t)
	good := dial(t, addr)
	assertPing(t, good)

	slow := dial(t, addr)
	send(t, slow, "*2\r\n$4\r\nECHO\r\n$100\r\nonly-part")
	assertPing(t, good)

	bad := dial(t, addr)
	send(t, bad, "*1\r\n$+4\r\nPING\r\n")
	readExactly(t, bad, "-ERR Protocol error: invalid bulk length\r\n")
	assertPing(t, good)

	// The half-open frame on the slow client is still parked, not lost.
	send(t, slow, string(bytes.Repeat([]byte("x"), 100-len("only-part")))+"\r\n")
	readExactly(t, slow, "$100\r\nonly-part"+strings.Repeat("x", 100-len("only-part"))+"\r\n")
}
