package testutil

import (
	"os"
	"strconv"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	code := m.Run()
	RemoveBuild()
	os.Exit(code)
}

// Start, kill, restart and ping must fit in 2 s (T1.02 acceptance), not
// counting the one-off build of the binary.
func TestStartKillRestartPing(t *testing.T) {
	BuildOnce(t)
	begin := time.Now()

	p := Start(t)
	ping(t, p)
	p.Kill()
	if !p.Exited() {
		t.Fatal("Kill returned but the process has not exited")
	}
	if _, err := Dial(p.Addr()); err == nil {
		t.Fatal("dial succeeded after Kill")
	}
	p.Restart(t)
	ping(t, p)

	if d := time.Since(begin); d > 2*time.Second {
		t.Fatalf("start, kill, restart, ping took %v, want under 2s", d)
	}
}

// Two servers in one test get different ports and different directories, and
// neither sees the other's keys.
func TestTwoServersAreIsolated(t *testing.T) {
	a, b := Start(t), Start(t)
	if a.Port == b.Port || a.Dir == b.Dir || a.DataDir() == b.DataDir() {
		t.Fatalf("servers share a port or dir: %d %s, %d %s", a.Port, a.Dir, b.Port, b.Dir)
	}
	ca, err := Dial(a.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer ca.Close()
	if v, err := ca.Do("SET", "k", "a"); err != nil || string(v.Str) != "OK" {
		t.Fatalf("SET on a = %s, %v", Show(v), err)
	}
	cb, err := Dial(b.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer cb.Close()
	if v, err := cb.Do("GET", "k"); err != nil || !v.Null {
		t.Fatalf("GET on b = %s, %v; want nil", Show(v), err)
	}
}

// Stop sends SIGTERM; the server must be gone within the 2 s grace period
// either way.
func TestStopEndsTheProcess(t *testing.T) {
	p := Start(t)
	p.Stop()
	if !p.Exited() {
		t.Fatal("process still running after Stop")
	}
}

// Info returns the parsed fields of one section.
func TestInfoReadsFields(t *testing.T) {
	p := Start(t)
	f := p.Info(t, "server")
	if f["redis_version"] == "" || f["tcp_port"] == "" {
		t.Fatalf("INFO server = %v, want redis_version and tcp_port", f)
	}
	if f["tcp_port"] != strconv.Itoa(p.Port) {
		t.Fatalf("tcp_port = %s, want %d", f["tcp_port"], p.Port)
	}
}

func TestWaitForPolls(t *testing.T) {
	n := 0
	WaitFor(t, time.Second, "third poll", func() bool { n++; return n == 3 })
	if n != 3 {
		t.Fatalf("polled %d times, want 3", n)
	}
}

func TestParseInfoRejectsMalformedPayloads(t *testing.T) {
	good := "# Server\r\nredis_version:1\r\n\r\n# Keyspace\r\ndb0:keys=1,expires=0\r\n"
	info, err := ParseInfo(good)
	if err != nil {
		t.Fatalf("ParseInfo(good) = %v", err)
	}
	if len(info.Sections) != 2 || info.Values["db0"] != "keys=1,expires=0" {
		t.Fatalf("ParseInfo(good) = %+v", info)
	}
	for _, bad := range []string{
		"# Server\r\nredis_version:1",                   // no trailing CRLF
		"# Server\r\nredis_version\r\n",                 // not key:value
		"redis_version:1\r\n",                           // field before a section
		"# Server\r\na:1\r\n# Stats\r\nb:2\r\n",         // no blank line between sections
		"# Server\r\na:1\r\n\r\n\r\n# Stats\r\nb:2\r\n", // double blank line
		"# Server\r\na:1\r\n\r\n# Stats\r\na:2\r\n",     // duplicate field
		"# Server\r\na:1\nb:2\r\n",                      // bare LF
	} {
		if _, err := ParseInfo(bad); err == nil {
			t.Errorf("ParseInfo(%q) accepted a malformed payload", bad)
		}
	}
}

func ping(t *testing.T, p *ServerProcess) {
	t.Helper()
	c, err := Dial(p.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	v, err := c.Do("PING")
	if err != nil || string(v.Str) != "PONG" {
		t.Fatalf("PING = %s, %v", Show(v), err)
	}
}
