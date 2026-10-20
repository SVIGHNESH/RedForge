package command

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"

	"github.com/SVIGHNESH/RedForge/internal/resp"
	"github.com/SVIGHNESH/RedForge/internal/server"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

// setReplyMembers parses an SMEMBERS or SINTER reply and returns the members
// sorted, since order is unspecified. A flat bulk array has the same wire
// format as a command frame, so resp.ParseCommand does the parsing.
func setReplyMembers(t *testing.T, raw string) []string {
	t.Helper()
	if raw == "*0\r\n" {
		return []string{}
	}
	items, n, err := resp.ParseCommand([]byte(raw))
	if err != nil || n != len(raw) {
		t.Fatalf("set reply %q did not parse: %v", raw, err)
	}
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = string(it)
	}
	sort.Strings(out)
	for i := 1; i < len(out); i++ {
		if out[i] == out[i-1] {
			t.Fatalf("set reply repeats member %q", out[i])
		}
	}
	return out
}

func setCtx() (*Ctx, *memoryAppender) {
	ctx := newCtx()
	mem := &memoryAppender{}
	ctx.Srv.Appenders = []server.Appender{mem}
	return ctx, mem
}

func setEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The session from the task file, run through the dispatcher.
func TestSetSession(t *testing.T) {
	ctx, _ := setCtx()
	steps := []struct {
		args []string
		want string
	}{
		{[]string{"SADD", "s", "a", "b", "a"}, ":2\r\n"},
		{[]string{"SISMEMBER", "s", "a"}, ":1\r\n"},
		{[]string{"SISMEMBER", "s", "zz"}, ":0\r\n"},
		{[]string{"SISMEMBER", "nokey", "a"}, ":0\r\n"},
		{[]string{"SADD", "t", "b", "c"}, ":2\r\n"},
		{[]string{"SINTER", "s", "t"}, "*1\r\n$1\r\nb\r\n"},
		{[]string{"SMEMBERS", "nokey"}, "*0\r\n"},
		{[]string{"SREM", "s", "a", "b"}, ":2\r\n"},
		{[]string{"SMEMBERS", "s"}, "*0\r\n"},
	}
	for _, s := range steps {
		if got := reply(t, ctx, s.args...); got != s.want {
			t.Fatalf("%v = %q, want %q", s.args, got, s.want)
		}
	}
	if ctx.Store.Exists("s") {
		t.Fatal("set with zero members must be deleted as a key")
	}
}

// Cardinality invariance: adding members that are already present, in any
// order and any number of times, never changes SMEMBERS, and SADD's reply is
// always exactly the number of members that were new.
func TestSetCardinalityUnderDuplicateAdds(t *testing.T) {
	ctx, _ := setCtx()
	rng := rand.New(rand.NewSource(7))
	ref := map[string]bool{}
	for step := 0; step < 500; step++ {
		args := []string{"SADD", "s"}
		fresh := map[string]bool{}
		for i := 0; i < 1+rng.Intn(5); i++ {
			m := fmt.Sprint(rng.Intn(40))
			args = append(args, m)
			if !ref[m] {
				fresh[m] = true
			}
		}
		want := fmt.Sprintf(":%d\r\n", len(fresh))
		if got := reply(t, ctx, args...); got != want {
			t.Fatalf("step %d %v = %q, want %q", step, args, got, want)
		}
		for m := range fresh {
			ref[m] = true
		}
	}
	var want []string
	for m := range ref {
		want = append(want, m)
	}
	sort.Strings(want)
	if got := setReplyMembers(t, reply(t, ctx, "SMEMBERS", "s")); !setEqual(got, want) {
		t.Fatalf("SMEMBERS = %v, want %v", got, want)
	}
}

// SINTER must equal the reference intersection for random sets of different
// sizes, whichever key holds the smallest set.
func TestSInterMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for trial := 0; trial < 50; trial++ {
		ctx, _ := setCtx()
		nkeys := 1 + rng.Intn(4)
		var refs []map[string]bool
		args := []string{"SINTER"}
		for k := 0; k < nkeys; k++ {
			key := fmt.Sprint("k", k)
			ref := map[string]bool{}
			add := []string{"SADD", key}
			for i := 0; i < 1+rng.Intn(30); i++ {
				m := fmt.Sprint(rng.Intn(20))
				ref[m] = true
				add = append(add, m)
			}
			reply(t, ctx, add...)
			refs = append(refs, ref)
			args = append(args, key)
		}
		want := []string{}
		for m := range refs[0] {
			inAll := true
			for _, r := range refs[1:] {
				if !r[m] {
					inAll = false
				}
			}
			if inAll {
				want = append(want, m)
			}
		}
		sort.Strings(want)
		if got := setReplyMembers(t, reply(t, ctx, args...)); !setEqual(got, want) {
			t.Fatalf("trial %d %v = %v, want %v", trial, args, got, want)
		}
	}
}

// A missing key is an empty set, so the intersection is empty and not an
// error, wherever the missing key appears. A key of the wrong type is an
// error even when another key is missing, as in Redis.
func TestSInterMissingAndWrongType(t *testing.T) {
	ctx, _ := setCtx()
	reply(t, ctx, "SADD", "s", "a", "b")
	ctx.Store.Put("str", &store.Entry{Type: store.TString, Val: &store.StringVal{B: []byte("x")}})
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"SINTER", "nokey"}, "*0\r\n"},
		{[]string{"SINTER", "s", "nokey"}, "*0\r\n"},
		{[]string{"SINTER", "nokey", "s"}, "*0\r\n"},
		{[]string{"SINTER", "s"}, ""},
		{[]string{"SINTER", "s", "s"}, ""},
		{[]string{"SINTER", "nokey", "str"}, "-WRONGTYPE Operation against a key holding the wrong kind of value\r\n"},
		{[]string{"SINTER", "s", "str"}, "-WRONGTYPE Operation against a key holding the wrong kind of value\r\n"},
	}
	for _, tc := range cases {
		got := reply(t, ctx, tc.args...)
		if tc.want == "" {
			if m := setReplyMembers(t, got); !setEqual(m, []string{"a", "b"}) {
				t.Errorf("%v = %v, want [a b]", tc.args, m)
			}
			continue
		}
		if got != tc.want {
			t.Errorf("%v = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestSetArityAndWrongType(t *testing.T) {
	ctx, mem := setCtx()
	ctx.Store.Put("str", &store.Entry{Type: store.TString, Val: &store.StringVal{B: []byte("x")}})
	wrong := "-WRONGTYPE Operation against a key holding the wrong kind of value\r\n"
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"SADD", "s"}, "-ERR wrong number of arguments for 'sadd' command\r\n"},
		{[]string{"SREM", "s"}, "-ERR wrong number of arguments for 'srem' command\r\n"},
		{[]string{"SISMEMBER", "s"}, "-ERR wrong number of arguments for 'sismember' command\r\n"},
		{[]string{"SMEMBERS"}, "-ERR wrong number of arguments for 'smembers' command\r\n"},
		{[]string{"SINTER"}, "-ERR wrong number of arguments for 'sinter' command\r\n"},
		{[]string{"SADD", "str", "a"}, wrong},
		{[]string{"SREM", "str", "a"}, wrong},
		{[]string{"SISMEMBER", "str", "a"}, wrong},
		{[]string{"SMEMBERS", "str"}, wrong},
	}
	for _, tc := range cases {
		if got := reply(t, ctx, tc.args...); got != tc.want {
			t.Errorf("%v = %q, want %q", tc.args, got, tc.want)
		}
	}
	if e, _ := ctx.Store.Lookup("str"); e.Type != store.TString {
		t.Error("WRONGTYPE must leave the string untouched")
	}
	if len(mem.recs) != 0 || ctx.Srv.CurrentSeq() != 0 {
		t.Fatalf("errors propagated %v", mem.recs)
	}
}

// SADD and SREM propagate only the members that changed state, and nothing
// when none did. Reads never propagate.
func TestSetPropagation(t *testing.T) {
	ctx, mem := setCtx()
	reply(t, ctx, "SADD", "s", "a", "b", "a")
	reply(t, ctx, "SADD", "s", "a", "b")
	reply(t, ctx, "SADD", "s", "b", "c")
	reply(t, ctx, "SISMEMBER", "s", "a")
	reply(t, ctx, "SMEMBERS", "s")
	reply(t, ctx, "SINTER", "s")
	reply(t, ctx, "SREM", "s", "zz")
	reply(t, ctx, "SREM", "nokey", "a")
	reply(t, ctx, "SREM", "s", "zz", "a", "a")
	reply(t, ctx, "SREM", "s", "b", "c")

	want := [][]string{
		{"SADD", "s", "a", "b"},
		{"SADD", "s", "c"},
		{"SREM", "s", "a"},
		{"SREM", "s", "b", "c"},
	}
	if len(mem.recs) != len(want) {
		t.Fatalf("propagated %v, want %v", mem.recs, want)
	}
	for i, w := range want {
		if mem.recs[i].seq != uint64(i+1) || !setEqual(mem.recs[i].args, w) {
			t.Fatalf("record %d = %+v, want seq %d %v", i, mem.recs[i], i+1, w)
		}
	}
	if ctx.Store.Exists("s") {
		t.Fatal("emptied set must be deleted as a key")
	}
}

// Stored members must not alias the request buffer, which the loop reuses.
func TestSAddCopiesMemberBytes(t *testing.T) {
	ctx, _ := setCtx()
	args := [][]byte{[]byte("SADD"), []byte("s"), []byte("member")}
	Dispatch(ctx, args)
	copy(args[2], "XXXXXX")
	if got := reply(t, ctx, "SISMEMBER", "s", "member"); got != ":1\r\n" {
		t.Fatalf("SISMEMBER after buffer reuse = %q, want :1", got)
	}
}
