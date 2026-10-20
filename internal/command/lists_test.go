package command

import (
	"reflect"
	"testing"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/store"
)

// The session from the task file, through the dispatcher. Every expected
// reply was checked byte for byte against the reference redis-server.
func TestListSession(t *testing.T) {
	ctx := newCtx()
	steps := []struct {
		args []string
		want string
	}{
		{[]string{"LPUSH", "l", "a", "b"}, ":2\r\n"},
		{[]string{"RPUSH", "l", "c"}, ":3\r\n"},
		{[]string{"LRANGE", "l", "0", "-1"}, "*3\r\n$1\r\nb\r\n$1\r\na\r\n$1\r\nc\r\n"},
		{[]string{"LPOP", "l"}, "$1\r\nb\r\n"},
		{[]string{"RPOP", "l"}, "$1\r\nc\r\n"},
		{[]string{"LLEN", "l"}, ":1\r\n"},
		{[]string{"RPOP", "l"}, "$1\r\na\r\n"},
		{[]string{"EXISTS", "l"}, ":0\r\n"},
		{[]string{"LLEN", "l"}, ":0\r\n"},
		{[]string{"LPOP", "l"}, "$-1\r\n"},
		{[]string{"LRANGE", "l", "0", "-1"}, "*0\r\n"},
	}
	for _, s := range steps {
		if got := reply(t, ctx, s.args...); got != s.want {
			t.Fatalf("%v = %q, want %q", s.args, got, s.want)
		}
	}
}

// LPUSH k a b c pushes a, then b, then c at the head, so the list reads c b a.
func TestMultiValuePushOrder(t *testing.T) {
	ctx := newCtx()
	reply(t, ctx, "LPUSH", "l", "a", "b", "c")
	reply(t, ctx, "RPUSH", "l", "x", "y")
	got := reply(t, ctx, "LRANGE", "l", "0", "-1")
	want := "*5\r\n$1\r\nc\r\n$1\r\nb\r\n$1\r\na\r\n$1\r\nx\r\n$1\r\ny\r\n"
	if got != want {
		t.Fatalf("LRANGE = %q, want %q", got, want)
	}
}

// LRANGE index rules, each case matching the reference server.
func TestLRangeIndexRules(t *testing.T) {
	ctx := newCtx()
	reply(t, ctx, "RPUSH", "l", "a", "b", "c")
	cases := []struct {
		start, stop, want string
	}{
		{"0", "-1", "*3\r\n$1\r\na\r\n$1\r\nb\r\n$1\r\nc\r\n"},
		{"-100", "100", "*3\r\n$1\r\na\r\n$1\r\nb\r\n$1\r\nc\r\n"},
		{"5", "10", "*0\r\n"},
		{"-1", "-2", "*0\r\n"},
		{"1", "1", "*1\r\n$1\r\nb\r\n"},
		{"-2", "-1", "*2\r\n$1\r\nb\r\n$1\r\nc\r\n"},
		{"9223372036854775807", "-9223372036854775808", "*0\r\n"},
		{"x", "1", "-ERR value is not an integer or out of range\r\n"},
		{"0", "99999999999999999999", "-ERR value is not an integer or out of range\r\n"},
	}
	for _, c := range cases {
		if got := reply(t, ctx, "LRANGE", "l", c.start, c.stop); got != c.want {
			t.Errorf("LRANGE l %s %s = %q, want %q", c.start, c.stop, got, c.want)
		}
	}
}

// Every list command on a key of another type is WRONGTYPE and leaves the key
// untouched; nothing propagates.
func TestListWrongTypeLeavesKeyAlone(t *testing.T) {
	ctx := newCtx()
	reply(t, ctx, "SET", "s", "v")
	mem := withAppender(ctx)
	for _, args := range [][]string{
		{"LPUSH", "s", "x"}, {"RPUSH", "s", "x"}, {"LPOP", "s"}, {"RPOP", "s"},
		{"LLEN", "s"}, {"LRANGE", "s", "0", "-1"},
	} {
		if got := reply(t, ctx, args...); got != "-"+msgWrongType+"\r\n" {
			t.Errorf("%v = %q, want WRONGTYPE", args, got)
		}
	}
	if got := reply(t, ctx, "GET", "s"); got != "$1\r\nv\r\n" {
		t.Fatalf("GET s = %q after list commands, want v", got)
	}
	if len(mem.recs) != 0 {
		t.Fatalf("WRONGTYPE propagated %v", propagated(mem))
	}
}

func TestListArity(t *testing.T) {
	ctx := newCtx()
	for _, args := range [][]string{
		{"LPUSH", "l"}, {"RPUSH", "l"}, {"LPOP"}, {"LPOP", "l", "2"},
		{"RPOP", "l", "2"}, {"LLEN"}, {"LRANGE", "l", "0"},
	} {
		got := reply(t, ctx, args...)
		if got[0] != '-' {
			t.Errorf("%v = %q, want an arity error", args, got)
		}
	}
}

// Pushes propagate as received; pops propagate only when something was
// popped; reads and failed commands never propagate.
func TestListPropagation(t *testing.T) {
	ctx := newCtx()
	mem := withAppender(ctx)
	reply(t, ctx, "LPOP", "missing")
	reply(t, ctx, "RPOP", "missing")
	reply(t, ctx, "LPUSH", "l", "a", "b")
	reply(t, ctx, "RPUSH", "l", "c")
	reply(t, ctx, "LLEN", "l")
	reply(t, ctx, "LRANGE", "l", "0", "-1")
	reply(t, ctx, "lpop", "l")
	reply(t, ctx, "RPOP", "l")
	reply(t, ctx, "RPOP", "l")
	reply(t, ctx, "RPOP", "l")
	want := [][]string{
		{"LPUSH", "l", "a", "b"},
		{"RPUSH", "l", "c"},
		{"lpop", "l"},
		{"RPOP", "l"},
		{"RPOP", "l"},
	}
	if got := propagated(mem); !reflect.DeepEqual(got, want) {
		t.Fatalf("propagated %v, want %v", got, want)
	}
	if seq := ctx.Srv.CurrentSeq(); seq != uint64(len(want)) {
		t.Fatalf("seq = %d, want %d", seq, len(want))
	}
}

// The pushed bytes must not alias the client's read buffer, which the loop
// reuses for the next command.
func TestPushCopiesValueBytes(t *testing.T) {
	ctx := newCtx()
	buf := []byte("hello")
	Dispatch(ctx, [][]byte{[]byte("RPUSH"), []byte("l"), buf})
	copy(buf, "XXXXX")
	if got := reply(t, ctx, "LRANGE", "l", "0", "-1"); got != "*1\r\n$5\r\nhello\r\n" {
		t.Fatalf("LRANGE = %q after the read buffer changed", got)
	}
}

// A pop that empties the list deletes the key, and the key keeps no TTL from
// a previous life.
func TestEmptyListIsDeleted(t *testing.T) {
	ctx := newCtx()
	reply(t, ctx, "RPUSH", "l", "a")
	reply(t, ctx, "EXPIRE", "l", "100")
	reply(t, ctx, "LPOP", "l")
	if ctx.Store.Exists("l") {
		t.Fatal("key still exists after its last element was popped")
	}
	if n := ctx.Store.ExpiringLen(); n != 0 {
		t.Fatalf("ExpiringLen = %d after the list was deleted, want 0", n)
	}
	reply(t, ctx, "RPUSH", "l", "b")
	if got := reply(t, ctx, "TTL", "l"); got != ":-1\r\n" {
		t.Fatalf("TTL of a recreated list = %q, want -1", got)
	}
}

// Pushing onto an existing list keeps its TTL, as in Redis.
func TestPushKeepsTTL(t *testing.T) {
	ctx := newCtx()
	reply(t, ctx, "RPUSH", "l", "a")
	reply(t, ctx, "EXPIRE", "l", "100")
	reply(t, ctx, "LPUSH", "l", "b")
	if got := reply(t, ctx, "TTL", "l"); got != ":100\r\n" {
		t.Fatalf("TTL after push = %q, want 100", got)
	}
}

// An expired list is invisible to every list command and a pop on it does
// not propagate.
func TestExpiredListIsInvisible(t *testing.T) {
	ctx, clock := newCtxWithClock()
	reply(t, ctx, "RPUSH", "l", "a", "b")
	reply(t, ctx, "EXPIRE", "l", "1")
	mem := withAppender(ctx)
	clock.Advance(2 * time.Second)
	ctx.Now = clock.NowMs()
	if got := reply(t, ctx, "LLEN", "l"); got != ":0\r\n" {
		t.Fatalf("LLEN of expired list = %q", got)
	}
	if got := reply(t, ctx, "LPOP", "l"); got != "$-1\r\n" {
		t.Fatalf("LPOP of expired list = %q", got)
	}
	if len(mem.recs) != 0 {
		t.Fatalf("expired pop propagated %v", propagated(mem))
	}
}

// Every list write goes through Store.Mutable, the snapshot copy-on-write
// point. A store that records Mutable calls proves it.
func TestListWritesUseMutable(t *testing.T) {
	ctx := newCtx()
	rec := &mutableRecorder{Store: ctx.Store}
	ctx.Store = rec
	reply(t, ctx, "RPUSH", "l", "a", "b")
	reply(t, ctx, "LPUSH", "l", "c")
	reply(t, ctx, "LPOP", "l")
	reply(t, ctx, "RPOP", "l")
	if rec.mutable != 4 {
		t.Fatalf("Mutable called %d times for 4 writes, want 4", rec.mutable)
	}
	reply(t, ctx, "LLEN", "l")
	reply(t, ctx, "LRANGE", "l", "0", "-1")
	if rec.mutable != 4 {
		t.Fatalf("a read called Mutable (%d calls)", rec.mutable)
	}
}

type mutableRecorder struct {
	store.Store
	mutable int
}

func (m *mutableRecorder) Mutable(key string) (*store.Entry, bool) {
	m.mutable++
	return m.Store.Mutable(key)
}

// List, hash and set reads count keyspace_hits and keyspace_misses the way
// the reference server does: one per read lookup, including a lookup that
// then fails the type check. Writes never count.
func TestCollectionReadsCountHitsAndMisses(t *testing.T) {
	ctx := newCtx()
	for _, args := range [][]string{
		{"RPUSH", "l", "a"}, {"HSET", "h", "f", "v"}, {"SADD", "s", "m"}, {"SADD", "t", "m"},
		{"LLEN", "l"}, {"LLEN", "nol"}, {"LRANGE", "l", "0", "-1"},
		{"HGET", "h", "f"}, {"HGET", "noh", "f"}, {"HLEN", "h"}, {"HGETALL", "h"},
		{"SISMEMBER", "s", "m"}, {"SMEMBERS", "nos"}, {"SINTER", "s", "t"}, {"SINTER", "s", "nos"},
		{"LPOP", "l"}, {"HGET", "l", "f"},
	} {
		reply(t, ctx, args...)
	}
	// Same sequence against the reference server: keyspace_hits:9,
	// keyspace_misses:5.
	if h, m := ctx.Srv.Stats.KeyspaceHits, ctx.Srv.Stats.KeyspaceMisses; h != 9 || m != 5 {
		t.Fatalf("hits=%d misses=%d, want 9 and 5 as the reference server reports", h, m)
	}
}
