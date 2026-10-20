package integration

import (
	"net"
	"os/exec"
	"strings"
	"testing"
)

// cli runs one redis-cli command against addr and returns its trimmed
// output. It is the stand-in for testutil.CLI until the T1.02 harness lands;
// the test is skipped where redis-cli is not installed (CI installs it).
func cli(t *testing.T, addr string, args ...string) string {
	t.Helper()
	path, err := exec.LookPath("redis-cli")
	if err != nil {
		t.Skip("redis-cli not installed")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("addr %q: %v", addr, err)
	}
	argv := append([]string{"-h", host, "-p", port}, args...)
	out, err := exec.Command(path, argv...).CombinedOutput()
	if err != nil {
		t.Fatalf("redis-cli %v: %v\n%s", args, err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

// The T4.02 session, through the real redis-cli: pushes at both ends, a full
// LRANGE, pops from both ends, and the key gone once the list is empty.
func TestListCLISession(t *testing.T) {
	addr := startServer(t)
	steps := []struct {
		args []string
		want string
	}{
		{[]string{"LPUSH", "l", "a", "b"}, "2"},
		{[]string{"RPUSH", "l", "c"}, "3"},
		{[]string{"LRANGE", "l", "0", "-1"}, "b\na\nc"},
		{[]string{"LPOP", "l"}, "b"},
		{[]string{"RPOP", "l"}, "c"},
		{[]string{"LLEN", "l"}, "1"},
		{[]string{"RPOP", "l"}, "a"},
		{[]string{"EXISTS", "l"}, "0"},
		{[]string{"LPOP", "l"}, ""},
	}
	for _, s := range steps {
		if got := cli(t, addr, s.args...); got != s.want {
			t.Fatalf("redis-cli %v = %q, want %q", s.args, got, s.want)
		}
	}
}
