package integration

import (
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/SVIGHNESH/RedForge/internal/testutil"
)

// M3 is done when the string and keyspace commands pass a session driven by
// the real redis-cli. --no-raw forces redis-cli's typed rendering ("(integer)
// 1", quoted bulks, "(nil)"), so the comparison also checks reply types, not
// just payloads.
func TestStringsCLISession(t *testing.T) {
	port := testutil.Start(t).Port
	steps := []struct {
		args []string
		want string
	}{
		{[]string{"SET", "name", "Vighnesh"}, "OK"},
		{[]string{"GET", "name"}, `"Vighnesh"`},
		{[]string{"EXISTS", "name"}, "(integer) 1"},
		{[]string{"EXISTS", "nope"}, "(integer) 0"},
		{[]string{"INCR", "counter"}, "(integer) 1"},
		{[]string{"INCR", "counter"}, "(integer) 2"},
		{[]string{"KEYS", "*"}, `1) "counter"` + "\n" + `2) "name"`},
		{[]string{"DEL", "name", "counter"}, "(integer) 2"},
		{[]string{"GET", "name"}, "(nil)"},
	}
	for _, s := range steps {
		got := testutil.CLI(t, port, append([]string{"--no-raw"}, s.args...)...)
		if s.args[0] == "KEYS" {
			got = sortKeysOutput(got)
		}
		if got != s.want {
			t.Fatalf("redis-cli --no-raw %s =\n%s\nwant\n%s", strings.Join(s.args, " "), got, s.want)
		}
	}
}

// sortKeysOutput sorts a redis-cli array rendering by element, then
// renumbers it, because KEYS returns keys in map order.
func sortKeysOutput(out string) string {
	lines := strings.Split(out, "\n")
	vals := make([]string, len(lines))
	for i, l := range lines {
		_, v, _ := strings.Cut(l, ") ")
		vals[i] = v
	}
	sort.Strings(vals)
	for i, v := range vals {
		lines[i] = strconv.Itoa(i+1) + ") " + v
	}
	return strings.Join(lines, "\n")
}
