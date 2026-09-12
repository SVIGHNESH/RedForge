package server

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"

	"github.com/SVIGHNESH/RedForge/internal/resp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// startTestServer brings up a loop on an ephemeral port and returns its
// address. Serve runs in its own goroutine because a test needs to keep
// driving connections while the loop runs; the loop itself still uses exactly
// one thread and spawns nothing per connection.
func startTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	s := New()
	// A stand-in for the dispatcher T0.06 installs: enough to prove framing.
	s.SetCommand(func(_ *Server, c *Client, args [][]byte) resp.Reply {
		switch strings.ToUpper(string(args[0])) {
		case "PING":
			return resp.PONG
		case "ECHO":
			return resp.Bulk(args[1])
		case "BIG":
			return resp.Bulk(bytes.Repeat([]byte("x"), 2*1024*1024))
		default:
			return resp.OK
		}
	})
	if err := s.Listen("127.0.0.1", 0); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	port, err := s.Port()
	if err != nil {
		t.Fatalf("Port: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Serve() }()
	t.Cleanup(func() {
		s.Stop()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Serve: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("Serve did not return within 2s of Stop")
		}
		s.Close()
	})
	return s, fmt.Sprintf("127.0.0.1:%d", port)
}

func TestInlineCommandRoundTrip(t *testing.T) {
	_, addr := startTestServer(t)
	conn := dial(t, addr)

	if _, err := conn.Write([]byte("PING\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := readN(t, conn, len("+PONG\r\n")); got != "+PONG\r\n" {
		t.Fatalf("reply = %q, want +PONG", got)
	}
}

// Two commands in one write must produce two replies: the loop drains
// everything the read buffer holds before it flushes.
func TestTwoCommandsInOneWrite(t *testing.T) {
	_, addr := startTestServer(t)
	conn := dial(t, addr)

	if _, err := conn.Write([]byte("*1\r\n$4\r\nPING\r\n*2\r\n$4\r\nECHO\r\n$2\r\nhi\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	want := "+PONG\r\n$2\r\nhi\r\n"
	if got := readN(t, conn, len(want)); got != want {
		t.Fatalf("replies = %q, want %q", got, want)
	}
}

// One command split across three writes must produce exactly one reply, and
// only after the last byte arrives. A partial frame must never be answered.
func TestCommandSplitAcrossThreeWrites(t *testing.T) {
	_, addr := startTestServer(t)
	conn := dial(t, addr)

	for _, chunk := range []string{"*2\r\n$4\r\n", "ECHO\r\n$2\r\n", "hi\r\n"} {
		if _, err := conn.Write([]byte(chunk)); err != nil {
			t.Fatalf("write %q: %v", chunk, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := readN(t, conn, len("$2\r\nhi\r\n")); got != "$2\r\nhi\r\n" {
		t.Fatalf("reply = %q, want $2\\r\\nhi", got)
	}
	// Nothing else may follow: the partial frames produced no reply of their own.
	conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	if n, err := conn.Read(make([]byte, 16)); err == nil {
		t.Fatalf("got %d unexpected extra bytes", n)
	}
}

// Pipelining, in order, at depth: 1000 PINGs in one write, 1000 +PONG back.
func TestThousandPipelinedCommands(t *testing.T) {
	_, addr := startTestServer(t)
	conn := dial(t, addr)

	const n = 1000
	var out bytes.Buffer
	for i := 0; i < n; i++ {
		out.WriteString("*1\r\n$4\r\nPING\r\n")
	}
	go func() { conn.Write(out.Bytes()) }()

	want := strings.Repeat("+PONG\r\n", n)
	if got := readN(t, conn, len(want)); got != want {
		t.Fatalf("pipelined replies differ from %d ordered +PONG", n)
	}
}

// Garbage must earn a protocol error and a closed connection, not a hung or
// desynchronised stream.
func TestGarbageIsAProtocolErrorAndCloses(t *testing.T) {
	_, addr := startTestServer(t)
	conn := dial(t, addr)

	if _, err := conn.Write([]byte("*1\r\n+notabulk\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	rest, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasPrefix(string(rest), "-ERR Protocol error:") {
		t.Fatalf("reply = %q, want a -ERR Protocol error", rest)
	}
	// io.ReadAll returned, so the server closed the connection itself.
}

// The reason for the whole epoll design: connection count must not drive
// thread or goroutine count. 100 concurrent clients must add none.
func TestHundredConnectionsAddNoGoroutines(t *testing.T) {
	s, addr := startTestServer(t)

	// Let the loop settle so the baseline does not include start-up churn.
	waitFor(t, func() bool { return s.ConnectedClients() == 0 })
	baseline := runtime.NumGoroutine()

	conns := make([]net.Conn, 0, 100)
	for i := 0; i < 100; i++ {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		conns = append(conns, c)
	}
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()

	waitFor(t, func() bool { return s.ConnectedClients() == 100 })
	if got := s.TotalConnections(); got < 100 {
		t.Errorf("TotalConnections = %d, want at least 100", got)
	}
	if got := runtime.NumGoroutine(); got > baseline {
		t.Fatalf("goroutines grew from %d to %d across 100 connections", baseline, got)
	}

	// Each one still works, so they are genuinely registered, not merely queued.
	for i, c := range conns {
		if _, err := c.Write([]byte("PING\r\n")); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		if got := readN(t, c, len("+PONG\r\n")); got != "+PONG\r\n" {
			t.Fatalf("conn %d replied %q, want +PONG", i, got)
		}
	}
}

// A peer that hangs up must cost the server nothing: no registry entry and no
// descriptor. A leak here is invisible until the server dies of EMFILE.
func TestClosedClientsLeakNoDescriptors(t *testing.T) {
	s, addr := startTestServer(t)
	waitFor(t, func() bool { return s.ConnectedClients() == 0 })
	baseline := openFDCount(t)

	for i := 0; i < 50; i++ {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		if _, err := c.Write([]byte("PING\r\n")); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		readN(t, c, len("+PONG\r\n"))
		c.Close()
	}

	waitFor(t, func() bool { return s.ConnectedClients() == 0 })
	waitFor(t, func() bool { return openFDCount(t) <= baseline })
	if got := openFDCount(t); got > baseline {
		t.Fatalf("open descriptors = %d, baseline %d", got, baseline)
	}
}

// A reply far larger than the socket buffer must still arrive whole, which
// exercises the short-write path and the EPOLLOUT registration.
func TestReplyLargerThanTheSocketBufferDrains(t *testing.T) {
	_, addr := startTestServer(t)
	conn := dial(t, addr)

	if _, err := conn.Write([]byte("BIG\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	const size = 2 * 1024 * 1024
	want := "$" + strconv.Itoa(size) + "\r\n"
	conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	got := make([]byte, len(want)+size+2)
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.HasPrefix(string(got), want) || !bytes.HasSuffix(got, []byte("\r\n")) {
		t.Fatal("large bulk reply did not arrive intact")
	}
	if body := got[len(want) : len(got)-2]; !bytes.Equal(body, bytes.Repeat([]byte("x"), size)) {
		t.Fatal("large bulk reply body differs")
	}
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

func readN(t *testing.T, conn net.Conn, n int) string {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, n)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(buf)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met within 5s")
}

func openFDCount(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("/proc", fmt.Sprint(os.Getpid()), "fd"))
	if err != nil {
		t.Skipf("cannot read /proc fd table: %v", err)
	}
	return len(entries)
}
