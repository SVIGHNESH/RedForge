package command

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/server"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

// startLoop runs the real event loop with the real dispatcher over a store on
// the wall clock, on an ephemeral port. observe, if not nil, runs on the loop
// thread every tick, which is the only race-free way for a test to read the
// live store while Serve is running. This stands in for the testutil
// ServerProcess harness (T1.02) until it lands.
func startLoop(t *testing.T, observe func(st store.Store, srv *server.Server)) (string, store.Store) {
	t.Helper()
	clock := store.RealClock{}
	st := store.New(clock)
	srv := server.New()
	Install(srv, st, clock)
	if observe != nil {
		srv.OnTick(func() { observe(st, srv) })
	}
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
		if err := <-done; err != nil {
			t.Errorf("Serve: %v", err)
		}
		srv.Close()
	})
	return fmt.Sprintf("127.0.0.1:%d", port), st
}

// testConn is a minimal RESP client for in-process loop tests.
type testConn struct {
	t    *testing.T
	conn net.Conn
	r    *bufio.Reader
}

func dialLoop(t *testing.T, addr string) *testConn {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return &testConn{t: t, conn: conn, r: bufio.NewReader(conn)}
}

// send writes raw protocol bytes, for pipelining many commands at once.
func (c *testConn) send(raw string) {
	c.t.Helper()
	if _, err := io.WriteString(c.conn, raw); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

// read returns the next reply: the line without its type byte for simple
// strings, errors and integers (errors keep a leading "-"), and the payload
// for bulk strings ("(nil)" for a null bulk). Arrays are not needed here.
func (c *testConn) read() string {
	c.t.Helper()
	c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := c.r.ReadString('\n')
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	line = strings.TrimSuffix(line, "\r\n")
	switch line[0] {
	case '+', ':':
		return line[1:]
	case '-':
		return line
	case '$':
		n, err := strconv.Atoi(line[1:])
		if err != nil {
			c.t.Fatalf("bad bulk header %q", line)
		}
		if n < 0 {
			return "(nil)"
		}
		buf := make([]byte, n+2)
		if _, err := io.ReadFull(c.r, buf); err != nil {
			c.t.Fatalf("read bulk: %v", err)
		}
		return string(buf[:n])
	}
	c.t.Fatalf("unexpected reply %q", line)
	return ""
}

// do sends one inline command and returns its reply.
func (c *testConn) do(cmd string) string {
	c.t.Helper()
	c.send(cmd + "\r\n")
	return c.read()
}

// Ten thousand keys with EXPIRE 1 that are never read again must be
// reclaimed by the tick sweep alone, and PING must stay fast while it runs.
func TestActiveSweepReclaimsUntouchedKeysAndPingStaysFast(t *testing.T) {
	type view struct{ keys, swept int64 }
	views := make(chan view, 1)
	addr, _ := startLoop(t, func(st store.Store, _ *server.Server) {
		v := view{keys: int64(st.Len()), swept: st.(store.Counters).SweepDeleted()}
		select {
		case <-views:
		default:
		}
		views <- v
	})

	c := dialLoop(t, addr)
	const n = 10_000
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "SET k%d v\r\nEXPIRE k%d 1\r\n", i, i)
	}
	c.send(b.String())
	for i := 0; i < 2*n; i++ {
		if got := c.read(); got != "OK" && got != "1" {
			t.Fatalf("load reply %d = %q", i, got)
		}
	}

	ping := dialLoop(t, addr)
	var rtts []time.Duration
	deadline := time.Now().Add(5 * time.Second)
	var last view
	for time.Now().Before(deadline) {
		start := time.Now()
		if got := ping.do("PING"); got != "PONG" {
			t.Fatalf("PING = %q", got)
		}
		rtts = append(rtts, time.Since(start))
		select {
		case last = <-views:
		default:
		}
		if last.keys == 0 && last.swept == n {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if last.keys != 0 {
		t.Fatalf("after 5 s the store still holds %d keys", last.keys)
	}
	if last.swept != n {
		t.Fatalf("sweep reclaimed %d keys, want %d (nothing else touched them)", last.swept, n)
	}

	p95 := percentile(rtts, 0.95)
	t.Logf("%d PINGs during the sweep: p95 %v, max %v", len(rtts), p95, percentile(rtts, 1))
	if raceEnabled {
		t.Log("race detector on: latency bound not asserted")
		return
	}
	if p95 > 2*time.Millisecond {
		t.Fatalf("PING p95 during the sweep = %v, want under 2 ms", p95)
	}
}

func percentile(ds []time.Duration, p float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	i := int(p * float64(len(s)-1))
	return s[i]
}
