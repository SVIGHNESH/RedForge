package command

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/aof"
	"github.com/SVIGHNESH/RedForge/internal/server"
	"github.com/SVIGHNESH/RedForge/internal/store"
	"github.com/SVIGHNESH/RedForge/internal/storetest"
)

// dumpStore renders every key with its type, value and expiry so two stores
// can be compared exactly.
func dumpStore(st store.Store) []string {
	var out []string
	st.ForEach(func(k string, e *store.Entry) bool {
		var v string
		switch val := e.Val.(type) {
		case *store.StringVal:
			v = string(val.B)
		case *store.ListVal:
			v = fmt.Sprint(stringsOf(val.Range(0, -1)))
		case *store.HashVal:
			var fs []string
			for f, x := range val.M {
				fs = append(fs, f+"="+string(x))
			}
			sort.Strings(fs)
			v = fmt.Sprint(fs)
		case *store.SetVal:
			var ms []string
			for m := range val.M {
				ms = append(ms, m)
			}
			sort.Strings(ms)
			v = fmt.Sprint(ms)
		}
		out = append(out, fmt.Sprintf("%s type=%d val=%s exp=%d", k, e.Type, v, e.ExpireAt))
		return true
	})
	sort.Strings(out)
	return out
}

func stringsOf(b [][]byte) []string {
	out := make([]string, len(b))
	for i, x := range b {
		out[i] = string(x)
	}
	return out
}

// logCommands runs cmds through Dispatch with a real log writer and returns
// the log path and the store they produced.
func logCommands(t *testing.T, cmds [][]string) (string, *Ctx) {
	t.Helper()
	dir := t.TempDir()
	ctx := newCtx()
	w, err := aof.Open(dir, aof.No, func() int64 { return ctx.Now })
	if err != nil {
		t.Fatal(err)
	}
	ctx.Srv.Appenders = []server.Appender{w}
	for _, c := range cmds {
		reply(t, ctx, c...)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, aof.FileName), ctx
}

// replayInto replays path into a fresh store whose clock reads nowMs and
// returns the store and the server.
func replayInto(t *testing.T, path string, nowMs int64) (store.Store, *server.Server) {
	t.Helper()
	clock := &storetest.FakeClock{}
	clock.Set(nowMs)
	st := store.New(clock)
	srv := server.New()
	st.(store.Loader).SetLoading(true)
	last, _, err := aof.Replay(path, 0, func(seq uint64, args [][]byte) {
		if err := Replay(srv, st, nowMs, args); err != nil {
			t.Errorf("replay seq %d: %v", seq, err)
		}
	})
	st.(store.Loader).SetLoading(false)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetSeq(last)
	return st, srv
}

var mixedWrites = [][]string{
	{"SET", "s", "hello"},
	{"SET", "n", "10"},
	{"INCR", "n"},
	{"INCR", "n"},
	{"RPUSH", "l", "a", "b", "c"},
	{"LPUSH", "l", "z"},
	{"RPOP", "l"},
	{"HSET", "h", "f1", "v1", "f2", "v2"},
	{"HDEL", "h", "f1"},
	{"SADD", "set", "x", "y", "z"},
	{"SREM", "set", "y"},
	{"SET", "gone", "1"},
	{"DEL", "gone"},
	{"SET", "ttl", "v"},
	{"EXPIRE", "ttl", "3600"},
	{"SET", "kept", "v"},
	{"EXPIRE", "kept", "3600"},
	{"PERSIST", "kept"},
}

// Replaying the log rebuilds exactly the keyspace the commands produced,
// TTLs included, and resumes the sequence counter at the last record.
func TestReplayRebuildsTheStore(t *testing.T) {
	path, live := logCommands(t, mixedWrites)
	st, srv := replayInto(t, path, live.Now)
	if got, want := dumpStore(st), dumpStore(live.Store); !reflect.DeepEqual(got, want) {
		t.Fatalf("replayed store\n %v\nwant\n %v", got, want)
	}
	if srv.CurrentSeq() != live.Srv.CurrentSeq() {
		t.Fatalf("seq after replay = %d, want %d", srv.CurrentSeq(), live.Srv.CurrentSeq())
	}
}

// Replaying the same log twice (two restarts) gives the same store, and a
// replay appends nothing to the log and consumes no sequence number.
func TestReplayIsIdempotentAndWritesNothing(t *testing.T) {
	path, live := logCommands(t, mixedWrites)
	before, _ := os.ReadFile(path)

	a, _ := replayInto(t, path, live.Now)
	b, _ := replayInto(t, path, live.Now)
	if !reflect.DeepEqual(dumpStore(a), dumpStore(b)) {
		t.Fatal("two replays of one log disagree")
	}

	// With the log itself registered as an appender, Replay still must
	// not append.
	w, err := aof.Open(filepath.Dir(path), aof.No, func() int64 { return live.Now })
	if err != nil {
		t.Fatal(err)
	}
	srv := server.New()
	srv.Appenders = []server.Appender{w}
	st := store.New(nil)
	for _, c := range mixedWrites[:3] {
		if err := Replay(srv, st, live.Now, dispatchArgs(c...)); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()
	after, _ := os.ReadFile(path)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("Replay appended to the log")
	}
	if srv.CurrentSeq() != 0 {
		t.Fatalf("Replay consumed seq %d", srv.CurrentSeq())
	}
}

// A key given a TTL and then persisted must survive a replay that happens
// long after the TTL would have fired. Without loading mode, the PERSIST
// record would find the key already expired and the key would be lost.
func TestReplayAfterTheDeadlineKeepsAPersistedKey(t *testing.T) {
	path, live := logCommands(t, [][]string{
		{"SET", "k", "v"},
		{"EXPIRE", "k", "1"},
		{"PERSIST", "k"},
		{"SET", "l", "x"},
		{"EXPIRE", "l", "1"},
	})
	later := live.Now + int64(time.Hour/time.Millisecond)
	st, _ := replayInto(t, path, later)
	if e, ok := st.Lookup("k"); !ok || e.ExpireAt != 0 {
		t.Fatalf("persisted key lost or still expiring after replay: %v %v", e, ok)
	}
	// l's deadline has passed: it is kept through replay with its past
	// deadline and dropped by recovery, counted as expired.
	if n := st.(store.Loader).DropExpired(); n != 1 {
		t.Fatalf("DropExpired = %d, want 1", n)
	}
	if st.Exists("l") {
		t.Fatal("expired key still present after DropExpired")
	}
	if got := st.(store.Counters).ExpiredKeys(); got != 1 {
		t.Fatalf("expired_keys = %d, want 1", got)
	}
}

// A PEXPIREAT record whose time has passed is stored, not acted on, while
// replaying; a client sending the same command still deletes the key.
func TestPastPexpireatIsKeptForRecoveryOnlyWhenReplaying(t *testing.T) {
	ctx := newCtx()
	reply(t, ctx, "SET", "k", "v")
	past := fmt.Sprint(ctx.Now - 1)
	if err := Replay(ctx.Srv, ctx.Store, ctx.Now, dispatchArgs("PEXPIREAT", "k", past)); err != nil {
		t.Fatal(err)
	}
	ctx.Store.(store.Loader).SetLoading(true)
	if e, ok := ctx.Store.Lookup("k"); !ok || e.ExpireAt != ctx.Now-1 {
		t.Fatalf("replayed past PEXPIREAT: entry %v %v, want kept with its deadline", e, ok)
	}
	ctx.Store.(store.Loader).SetLoading(false)

	reply(t, ctx, "SET", "c", "v")
	if got := reply(t, ctx, "PEXPIREAT", "c", past); got != ":1\r\n" {
		t.Fatalf("client PEXPIREAT = %q", got)
	}
	ctx.Store.(store.Loader).SetLoading(true)
	if _, ok := ctx.Store.Lookup("c"); ok {
		t.Fatal("client PEXPIREAT in the past did not delete the key")
	}
}

func TestReplayRejectsBadRecords(t *testing.T) {
	srv, st := server.New(), store.New(nil)
	for _, args := range [][]string{{"NOPE", "x"}, {"SET", "a"}, {"GET", "a"}, {"INCR", "s"}} {
		if args[0] == "INCR" {
			st.Put("s", &store.Entry{Type: store.TString, Val: &store.StringVal{B: []byte("abc")}})
		}
		if err := Replay(srv, st, 0, dispatchArgs(args...)); err == nil {
			t.Errorf("Replay(%v) succeeded, want an error", args)
		}
	}
}
