package testutil

import (
	"runtime"
	"strconv"
	"testing"
	"time"
)

func TestClientDoAndHelpers(t *testing.T) {
	c := Start(t).Client(t)
	if v := c.MustDo(t, "PING"); string(v.Str) != "PONG" {
		t.Fatalf("PING = %s", Show(v))
	}
	if got := c.MustBulk(t, "ECHO", "hi"); got != "hi" {
		t.Fatalf("ECHO = %q", got)
	}
	c.MustOK(t, "SET", "k", "1")
	if n := c.MustInt(t, "INCR", "k"); n != 2 {
		t.Fatalf("INCR = %d", n)
	}
	c.MustNil(t, "GET", "missing")
	c.MustError(t, "ERR unknown command", "NOPE")
	if got := c.MustStrings(t, "KEYS", "*"); len(got) != 1 || got[0] != "k" {
		t.Fatalf("KEYS = %v", got)
	}
}

// A bulk reply larger than one socket read must be reassembled.
func TestClientReadsLargeReplies(t *testing.T) {
	c := Start(t).Client(t)
	big := make([]byte, 1<<20)
	for i := range big {
		big[i] = byte('a' + i%26)
	}
	c.MustOK(t, "SET", "big", string(big))
	if got := c.MustBulk(t, "GET", "big"); got != string(big) {
		t.Fatalf("GET big returned %d bytes, want %d", len(got), len(big))
	}
}

// T1.03 acceptance: a pipeline of 1000 commands returns 1000 replies in
// issue order.
func TestPipelineOfThousandKeepsOrder(t *testing.T) {
	c := Start(t).Client(t)
	cmds := make([][]string, 1000)
	for i := range cmds {
		cmds[i] = []string{"ECHO", strconv.Itoa(i)}
	}
	replies, err := c.Pipeline(cmds)
	if err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1000 {
		t.Fatalf("got %d replies, want 1000", len(replies))
	}
	for i, v := range replies {
		if string(v.Str) != strconv.Itoa(i) {
			t.Fatalf("reply %d = %s, want %d", i, Show(v), i)
		}
	}
}

// T1.03 acceptance: 100 clients opened in a loop and closed leak no
// goroutines in the test process.
func TestHundredClientsLeakNoGoroutines(t *testing.T) {
	p := Start(t)
	before := runtime.NumGoroutine()
	for i := 0; i < 100; i++ {
		c, err := Dial(p.Addr())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Do("PING"); err != nil {
			t.Fatal(err)
		}
		c.Close()
	}
	WaitFor(t, 2*time.Second, "goroutines to settle", func() bool {
		return runtime.NumGoroutine() <= before
	})
}

func TestCLIRunsRealRedisCLI(t *testing.T) {
	p := Start(t)
	if got := CLI(t, p.Port, "PING"); got != "PONG" {
		t.Fatalf("redis-cli PING = %q", got)
	}
	if got := CLI(t, p.Port, "--no-raw", "GET", "nope"); got != "(nil)" {
		t.Fatalf("redis-cli --no-raw GET nope = %q", got)
	}
}
