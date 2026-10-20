package command

import (
	"testing"

	"github.com/SVIGHNESH/RedForge/internal/resp"
	"github.com/SVIGHNESH/RedForge/internal/server"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

// hashReplyMap parses an HGETALL reply into a map, since field order is
// unspecified. A command frame and a flat bulk array share one wire format,
// so resp.ParseCommand does the parsing.
func hashReplyMap(t *testing.T, raw string) map[string]string {
	t.Helper()
	if raw == "*0\r\n" {
		return map[string]string{}
	}
	items, n, err := resp.ParseCommand([]byte(raw))
	if err != nil || n != len(raw) {
		t.Fatalf("HGETALL reply %q did not parse: %v", raw, err)
	}
	if len(items)%2 != 0 {
		t.Fatalf("HGETALL reply has odd length %d", len(items))
	}
	out := map[string]string{}
	for i := 0; i < len(items); i += 2 {
		if _, dup := out[string(items[i])]; dup {
			t.Fatalf("HGETALL repeated field %q", items[i])
		}
		out[string(items[i])] = string(items[i+1])
	}
	return out
}

func hashCtx() (*Ctx, *memoryAppender) {
	ctx := newCtx()
	mem := &memoryAppender{}
	ctx.Srv.Appenders = []server.Appender{mem}
	return ctx, mem
}

// The session from the task file, run through the dispatcher.
func TestHashSession(t *testing.T) {
	ctx, _ := hashCtx()
	steps := []struct {
		args []string
		want string
	}{
		{[]string{"HSET", "h", "a", "1", "b", "2"}, ":2\r\n"},
		{[]string{"HSET", "h", "a", "9"}, ":0\r\n"},
		{[]string{"HGET", "h", "a"}, "$1\r\n9\r\n"},
		{[]string{"HGET", "h", "zz"}, "$-1\r\n"},
		{[]string{"HGET", "nokey", "a"}, "$-1\r\n"},
		{[]string{"HLEN", "h"}, ":2\r\n"},
		{[]string{"HLEN", "nokey"}, ":0\r\n"},
		{[]string{"HGETALL", "nokey"}, "*0\r\n"},
		{[]string{"HDEL", "h", "a", "b"}, ":2\r\n"},
		{[]string{"HGETALL", "h"}, "*0\r\n"},
	}
	for _, s := range steps {
		if got := reply(t, ctx, s.args...); got != s.want {
			t.Fatalf("%v = %q, want %q", s.args, got, s.want)
		}
	}
	if ctx.Store.Exists("h") {
		t.Fatal("hash with zero fields must be deleted as a key")
	}
}

func TestHGetAllReturnsEveryPair(t *testing.T) {
	ctx, _ := hashCtx()
	reply(t, ctx, "HSET", "h", "a", "1", "b", "2", "c", "3")
	reply(t, ctx, "HSET", "h", "b", "20")
	got := hashReplyMap(t, reply(t, ctx, "HGETALL", "h"))
	want := map[string]string{"a": "1", "b": "20", "c": "3"}
	if len(got) != len(want) {
		t.Fatalf("HGETALL = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("HGETALL = %v, want %v", got, want)
		}
	}
}

// Invariant: HLEN equals the number of distinct fields ever set minus those
// deleted, whatever mix of new fields, updates and repeats one HSET carries.
func TestHashLenTracksDistinctFields(t *testing.T) {
	ctx, _ := hashCtx()
	if got := reply(t, ctx, "HSET", "h", "a", "1", "a", "2", "b", "3"); got != ":2\r\n" {
		t.Fatalf("HSET with a repeated field = %q, want :2", got)
	}
	if got := reply(t, ctx, "HGET", "h", "a"); got != "$1\r\n2\r\n" {
		t.Fatalf("last write must win, HGET = %q", got)
	}
	if got := reply(t, ctx, "HDEL", "h", "a", "a", "missing"); got != ":1\r\n" {
		t.Fatalf("HDEL a a missing = %q, want :1", got)
	}
	if got := reply(t, ctx, "HLEN", "h"); got != ":1\r\n" {
		t.Fatalf("HLEN = %q, want :1", got)
	}
}

func TestHashArityAndWrongType(t *testing.T) {
	ctx, mem := hashCtx()
	ctx.Store.Put("s", &store.Entry{Type: store.TString, Val: &store.StringVal{B: []byte("x")}})
	wrong := "-WRONGTYPE Operation against a key holding the wrong kind of value\r\n"
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"HSET", "h", "a"}, "-ERR wrong number of arguments for 'hset' command\r\n"},
		{[]string{"HSET", "h", "a", "1", "b"}, "-ERR wrong number of arguments for 'hset' command\r\n"},
		{[]string{"HGET", "h"}, "-ERR wrong number of arguments for 'hget' command\r\n"},
		{[]string{"HDEL", "h"}, "-ERR wrong number of arguments for 'hdel' command\r\n"},
		{[]string{"HLEN"}, "-ERR wrong number of arguments for 'hlen' command\r\n"},
		{[]string{"HGETALL", "h", "x"}, "-ERR wrong number of arguments for 'hgetall' command\r\n"},
		{[]string{"HSET", "s", "a", "1"}, wrong},
		{[]string{"HGET", "s", "a"}, wrong},
		{[]string{"HDEL", "s", "a"}, wrong},
		{[]string{"HLEN", "s"}, wrong},
		{[]string{"HGETALL", "s"}, wrong},
	}
	for _, tc := range cases {
		if got := reply(t, ctx, tc.args...); got != tc.want {
			t.Errorf("%v = %q, want %q", tc.args, got, tc.want)
		}
	}
	if ctx.Store.Exists("h") {
		t.Error("a rejected HSET must not create the key")
	}
	if e, _ := ctx.Store.Lookup("s"); e.Type != store.TString {
		t.Error("WRONGTYPE must leave the string untouched")
	}
	if len(mem.recs) != 0 || ctx.Srv.CurrentSeq() != 0 {
		t.Fatalf("errors propagated %v", mem.recs)
	}
}

// HSET propagates as received; HDEL propagates only the fields that existed
// and nothing at all when none did; reads never propagate.
func TestHashPropagation(t *testing.T) {
	ctx, mem := hashCtx()
	reply(t, ctx, "HSET", "h", "a", "1", "b", "2")
	reply(t, ctx, "HGET", "h", "a")
	reply(t, ctx, "HLEN", "h")
	reply(t, ctx, "HGETALL", "h")
	reply(t, ctx, "HDEL", "h", "zz")
	reply(t, ctx, "HDEL", "nokey", "a")
	reply(t, ctx, "HDEL", "h", "zz", "a", "yy")
	reply(t, ctx, "HDEL", "h", "b")

	want := [][]string{
		{"HSET", "h", "a", "1", "b", "2"},
		{"HDEL", "h", "a"},
		{"HDEL", "h", "b"},
	}
	if len(mem.recs) != len(want) {
		t.Fatalf("propagated %v, want %v", mem.recs, want)
	}
	for i, w := range want {
		if mem.recs[i].seq != uint64(i+1) || !hashEqualArgs(mem.recs[i].args, w) {
			t.Fatalf("record %d = %+v, want seq %d %v", i, mem.recs[i], i+1, w)
		}
	}
}

// The stored value must not alias the request buffer, which the loop reuses
// for the next read.
func TestHSetCopiesValueBytes(t *testing.T) {
	ctx, _ := hashCtx()
	args := [][]byte{[]byte("HSET"), []byte("h"), []byte("f"), []byte("value")}
	Dispatch(ctx, args)
	copy(args[3], "XXXXX")
	if got := reply(t, ctx, "HGET", "h", "f"); got != "$5\r\nvalue\r\n" {
		t.Fatalf("HGET after buffer reuse = %q, want value", got)
	}
}

func hashEqualArgs(a, b []string) bool {
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
