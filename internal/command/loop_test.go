package command

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/resp"
	"github.com/SVIGHNESH/RedForge/internal/server"
	"github.com/SVIGHNESH/RedForge/internal/store"
	"github.com/SVIGHNESH/RedForge/internal/testutil"
)

// startLoop runs the real event loop with the real dispatcher over a store on
// the wall clock, on an ephemeral port. observe, if not nil, runs on the loop
// thread every tick, which is the only race-free way for a test to read the
// live store while Serve is running, which is why these tests run the loop in
// process instead of through testutil.Start.
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

// testConn adapts testutil.Client to the string replies these loop tests
// compare against: the text of simple strings and integers, "-"-prefixed
// errors, bulk payloads, and "(nil)" for a null bulk.
type testConn struct {
	t *testing.T
	c *testutil.Client
}

func dialLoop(t *testing.T, addr string) *testConn {
	t.Helper()
	return &testConn{t: t, c: testutil.MustDial(t, addr)}
}

// send writes raw protocol bytes, for pipelining many commands at once.
func (c *testConn) send(raw string) {
	c.t.Helper()
	if err := c.c.Send([]byte(raw)); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

// read returns the next reply rendered as a string.
func (c *testConn) read() string {
	c.t.Helper()
	v, err := c.c.Read()
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	switch {
	case v.Kind == resp.KindError:
		return "-" + string(v.Str)
	case v.Kind == resp.KindInteger:
		return strconv.FormatInt(v.Int, 10)
	case v.Kind == resp.KindBulk && v.Null:
		return "(nil)"
	case v.Kind == resp.KindSimpleString || v.Kind == resp.KindBulk:
		return string(v.Str)
	}
	c.t.Fatalf("unexpected reply %s", testutil.Show(v))
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
	testutil.SkipIfShort(t, "waits about 3 s of wall clock for 10000 keys to expire")
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
