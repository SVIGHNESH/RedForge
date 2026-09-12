package server

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
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

func TestEchoRoundTrip(t *testing.T) {
	_, addr := startTestServer(t)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("hello loop\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := readN(t, conn, len("hello loop\r\n"))
	if got != "hello loop\r\n" {
		t.Fatalf("echo = %q, want %q", got, "hello loop\r\n")
	}
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
		msg := fmt.Sprintf("c%d\n", i)
		if _, err := c.Write([]byte(msg)); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		if got := readN(t, c, len(msg)); got != msg {
			t.Fatalf("conn %d echoed %q, want %q", i, got, msg)
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
		if _, err := c.Write([]byte("x\n")); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		readN(t, c, 2)
		c.Close()
	}

	waitFor(t, func() bool { return s.ConnectedClients() == 0 })
	waitFor(t, func() bool { return openFDCount(t) <= baseline })
	if got := openFDCount(t); got > baseline {
		t.Fatalf("open descriptors = %d, baseline %d", got, baseline)
	}
}

func TestWritesLargerThanTheSocketBufferDrain(t *testing.T) {
	_, addr := startTestServer(t)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	payload := bytes.Repeat([]byte("abcdefgh"), 256*1024) // 2 MB, well past any socket buffer
	go func() { conn.Write(payload) }()

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("echoed payload differs from what was sent")
	}
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
