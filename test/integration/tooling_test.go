package integration

import (
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/testutil"
)

// The MS0 exit criteria (T0.09), locked in so they cannot regress. redis-cli
// 7.x sends COMMAND DOCS on an interactive connect and redis-benchmark sends
// CONFIG GET; both must get an error reply and carry on.

// Check 1: redis-cli -p PORT PING prints PONG.
func TestToolingRedisCLIPing(t *testing.T) {
	p := testutil.Start(t)
	if got := testutil.CLI(t, p.Port, "PING"); got != "PONG" {
		t.Fatalf("redis-cli PING = %q, want PONG", got)
	}
}

// Check 2: an interactive redis-cli session starts without hanging, answers
// PING and exits on quit. redis-cli only behaves interactively on a
// terminal, so it runs under script(1), which gives it a pseudo-terminal.
func TestToolingRedisCLIInteractive(t *testing.T) {
	cli := testutil.RedisCLI(t)
	script, err := exec.LookPath("script")
	if err != nil {
		t.Fatalf("script(1) from util-linux is needed for the interactive check: %v", err)
	}
	p := testutil.Start(t)
	cmd := exec.Command(script, "-qec", cli+" -p "+strconv.Itoa(p.Port), "/dev/null")
	// TERM=dumb stops redis-cli's line editor from querying the cursor
	// position, which needs a real terminal to answer. stdin is still a
	// terminal, so redis-cli still runs interactively and sends COMMAND
	// DOCS on connect.
	cmd.Env = append(os.Environ(), "TERM=dumb")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out := &strings.Builder{}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Give redis-cli time to connect and send COMMAND DOCS before typing.
	time.Sleep(300 * time.Millisecond)
	io.WriteString(stdin, "PING\n")
	time.Sleep(200 * time.Millisecond)
	io.WriteString(stdin, "quit\n")
	stdin.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("interactive redis-cli: %v\n%s", err, out)
		}
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		t.Fatalf("interactive redis-cli hung:\n%s", out)
	}
	if !strings.Contains(out.String(), "PONG") {
		t.Fatalf("interactive session never printed PONG:\n%s", out)
	}
	if !strings.Contains(out.String(), "127.0.0.1:"+strconv.Itoa(p.Port)+">") {
		t.Fatalf("interactive session never showed the prompt:\n%s", out)
	}
}

// benchLine matches one redis-benchmark -q result line, for example
// "PING_INLINE: 94339.62 requests per second, p50=0.271 msec".
var benchLine = regexp.MustCompile(`^(PING_INLINE|PING_MBULK|PING_BULK): [0-9.]+ requests per second`)

// Check 3: redis-benchmark -t ping runs to completion. It warns that CONFIG
// GET failed, which is expected, and must still report both PING variants.
func TestToolingRedisBenchmarkPing(t *testing.T) {
	testutil.RedisCLI(t) // redis-benchmark ships with redis-cli in redis-tools
	bench, err := exec.LookPath("redis-benchmark")
	if err != nil {
		t.Fatalf("redis-benchmark not found next to redis-cli: %v", err)
	}
	p := testutil.Start(t)
	out, err := exec.Command(bench, "-p", strconv.Itoa(p.Port), "-t", "ping", "-n", "10000", "-q").CombinedOutput()
	if err != nil {
		t.Fatalf("redis-benchmark: %v\n%s", err, out)
	}
	results := 0
	for _, line := range strings.Split(string(out), "\n") {
		// -q rewrites progress with carriage returns; the result is the
		// text after the last one.
		if i := strings.LastIndex(line, "\r"); i >= 0 {
			line = line[i+1:]
		}
		if benchLine.MatchString(strings.TrimSpace(line)) {
			results++
		}
	}
	if results != 2 {
		t.Fatalf("redis-benchmark reported %d PING results, want 2:\n%s", results, out)
	}
}

// Check 4: two inline PINGs in one write get two +PONG replies, which proves
// pipelining and inline commands. The task runs this through nc; the test
// writes the same bytes on a raw socket so it needs no nc.
func TestToolingInlinePipelinedPing(t *testing.T) {
	conn := dial(t, startServer(t))
	send(t, conn, "PING\r\nPING\r\n")
	readExactly(t, conn, "+PONG\r\n+PONG\r\n")
}
