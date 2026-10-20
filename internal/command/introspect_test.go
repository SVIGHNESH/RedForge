package command

import (
	"bufio"
	"io"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/testutil"
)

// decodeReply turns one RESP2 reply into Go values: simple strings and bulk
// strings become string, integers int64, arrays []any, nulls nil, and errors
// a respError. It fails the test on anything malformed or left over, so it
// doubles as a protocol check.
type respError string

func decodeReply(t *testing.T, raw []byte) any {
	t.Helper()
	r := bufio.NewReader(strings.NewReader(string(raw)))
	v := decodeOne(t, r)
	if rest, _ := io.ReadAll(r); len(rest) != 0 {
		t.Fatalf("trailing bytes after reply: %q", rest)
	}
	return v
}

func decodeOne(t *testing.T, r *bufio.Reader) any {
	t.Helper()
	line, err := r.ReadString('\n')
	if err != nil || !strings.HasSuffix(line, "\r\n") || len(line) < 3 {
		t.Fatalf("bad RESP line %q: %v", line, err)
	}
	body := line[1 : len(line)-2]
	switch line[0] {
	case '+':
		return body
	case '-':
		return respError(body)
	case ':':
		n, err := strconv.ParseInt(body, 10, 64)
		if err != nil {
			t.Fatalf("bad integer %q", body)
		}
		return n
	case '$':
		n, err := strconv.Atoi(body)
		if err != nil {
			t.Fatalf("bad bulk length %q", body)
		}
		if n < 0 {
			return nil
		}
		buf := make([]byte, n+2)
		if _, err := io.ReadFull(r, buf); err != nil || string(buf[n:]) != "\r\n" {
			t.Fatalf("bad bulk payload: %v", err)
		}
		return string(buf[:n])
	case '*':
		n, err := strconv.Atoi(body)
		if err != nil {
			t.Fatalf("bad array length %q", body)
		}
		if n < 0 {
			return nil
		}
		out := make([]any, n)
		for i := range out {
			out[i] = decodeOne(t, r)
		}
		return out
	}
	t.Fatalf("unknown RESP type in %q", line)
	return nil
}

func TestCommandCountIsTheTableSize(t *testing.T) {
	ctx := newCtx()
	if got, want := reply(t, ctx, "COMMAND", "COUNT"), ":"+strconv.Itoa(len(table))+"\r\n"; got != want {
		t.Fatalf("COMMAND COUNT = %q, want %q", got, want)
	}
	if got := reply(t, ctx, "command", "count"); got != ":"+strconv.Itoa(len(table))+"\r\n" {
		t.Fatalf("lower-case COMMAND COUNT = %q", got)
	}
	if got := reply(t, ctx, "COMMAND", "COUNT", "x"); got != "-ERR wrong number of arguments for 'command|count' command\r\n" {
		t.Fatalf("COMMAND COUNT x = %q", got)
	}
}

// COMMAND lists every registered command exactly once, in the Redis shape,
// with flags that agree with the table.
func TestCommandListsTheRealTable(t *testing.T) {
	ctx := newCtx()
	entries, ok := decodeReply(t, Dispatch(ctx, dispatchArgs("COMMAND")).AppendTo(nil)).([]any)
	if !ok {
		t.Fatal("COMMAND did not return an array")
	}
	if len(entries) != len(table) {
		t.Fatalf("COMMAND listed %d commands, table has %d", len(entries), len(table))
	}
	var seen []string
	for _, e := range entries {
		f, ok := e.([]any)
		if !ok || len(f) != 7 {
			t.Fatalf("entry %v is not a 7-element array", e)
		}
		name := f[0].(string)
		cmd, ok := Lookup(name)
		if !ok || name != strings.ToLower(cmd.Name) {
			t.Fatalf("entry name %q is not a registered lower-case command", name)
		}
		seen = append(seen, name)
		if f[1].(int64) != int64(cmd.Arity) {
			t.Errorf("%s arity = %d, want %d", name, f[1], cmd.Arity)
		}
		flags := map[string]bool{}
		for _, fl := range f[2].([]any) {
			flags[fl.(string)] = true
		}
		if flags["write"] != (cmd.Flags&Write != 0) || flags["readonly"] != (cmd.Flags&ReadOnly != 0) {
			t.Errorf("%s flags %v disagree with the table", name, flags)
		}
		if flags["write"] == flags["readonly"] {
			t.Errorf("%s must be exactly one of write or readonly, got %v", name, flags)
		}
		if acl := f[6].([]any); len(acl) != 0 {
			t.Errorf("%s ACL categories = %v, want empty", name, acl)
		}
	}
	if !sort.StringsAreSorted(seen) {
		t.Errorf("COMMAND order is not stable: %v", seen)
	}
}

func TestCommandKeyPositions(t *testing.T) {
	cases := map[string][3]int64{
		"GET": {1, 1, 1}, "SET": {1, 1, 1}, "DEL": {1, -1, 1}, "EXISTS": {1, -1, 1},
		"PING": {0, 0, 0}, "INFO": {0, 0, 0}, "COMMAND": {0, 0, 0},
	}
	for name, want := range cases {
		e := decodeReply(t, commandEntry(table[name]).AppendTo(nil)).([]any)
		got := [3]int64{e[3].(int64), e[4].(int64), e[5].(int64)}
		if got != want {
			t.Errorf("%s key positions = %v, want %v", name, got, want)
		}
	}
}

// Every other subcommand, DOCS included, is an error worded like Redis's,
// echoing the subcommand as sent. redis-cli tolerates it on connect.
func TestCommandOtherSubcommandsAreErrors(t *testing.T) {
	ctx := newCtx()
	mem := withAppender(ctx)
	for _, sub := range []string{"DOCS", "docs", "INFO", "HELP"} {
		want := "-ERR unknown subcommand '" + sub + "'. Try COMMAND HELP.\r\n"
		if got := reply(t, ctx, "COMMAND", sub); got != want {
			t.Errorf("COMMAND %s = %q, want %q", sub, got, want)
		}
	}
	if got := reply(t, ctx, "COMMAND", "x\r\n+OK"); strings.Count(got, "\r\n") != 1 {
		t.Errorf("subcommand error carried an injected frame: %q", got)
	}
	if len(mem.recs) != 0 {
		t.Fatal("COMMAND propagated")
	}
}

// A real redis-cli fed commands on stdin connects, runs them against the
// loop and exits. The interactive start-up path, where redis-cli 7 sends
// COMMAND DOCS, needs a terminal and is checked by hand in the PR and by the
// T0.09 smoke test.
func TestRedisCLIRunsCommandsFromStdin(t *testing.T) {
	cli := testutil.RedisCLI(t)
	addr, _ := startLoop(t, nil)
	host, port, _ := strings.Cut(addr, ":")
	cmd := exec.Command(cli, "-h", host, "-p", port)
	cmd.Stdin = strings.NewReader("SET greeting hello\nGET greeting\nCOMMAND COUNT\n")
	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		out, err = cmd.CombinedOutput()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		t.Fatal("redis-cli hung")
	}
	if err != nil {
		t.Fatalf("redis-cli: %v\n%s", err, out)
	}
	want := "OK\nhello\n" + strconv.Itoa(len(table)) + "\n"
	if string(out) != want {
		t.Fatalf("redis-cli output = %q, want %q", out, want)
	}
}
