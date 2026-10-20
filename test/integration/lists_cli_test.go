package integration

import (
	"testing"

	"github.com/SVIGHNESH/RedForge/internal/testutil"
)

// The T4.02 session, through the real redis-cli: pushes at both ends, a full
// LRANGE, pops from both ends, and the key gone once the list is empty.
func TestListCLISession(t *testing.T) {
	port := testutil.Start(t).Port
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
		if got := testutil.CLI(t, port, s.args...); got != s.want {
			t.Fatalf("redis-cli %v = %q, want %q", s.args, got, s.want)
		}
	}
}
