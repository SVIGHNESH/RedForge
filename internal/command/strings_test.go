package command

import (
	"reflect"
	"testing"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/server"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

// withAppender attaches a fresh memoryAppender to ctx and returns it.
func withAppender(ctx *Ctx) *memoryAppender {
	mem := &memoryAppender{}
	ctx.Srv.Appenders = []server.Appender{mem}
	return mem
}

// propagated flattens the appender's records into their argument lists.
func propagated(mem *memoryAppender) [][]string {
	out := make([][]string, len(mem.recs))
	for i, r := range mem.recs {
		out[i] = r.args
	}
	return out
}

func TestSetGetDelExistsSession(t *testing.T) {
	ctx := newCtx()
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"SET", "a", "1"}, "+OK\r\n"},
		{[]string{"GET", "a"}, "$1\r\n1\r\n"},
		{[]string{"EXISTS", "a", "b"}, ":1\r\n"},
		{[]string{"DEL", "a", "b"}, ":1\r\n"},
		{[]string{"GET", "a"}, "$-1\r\n"},
		{[]string{"EXISTS", "a"}, ":0\r\n"},
		{[]string{"set", "e", ""}, "+OK\r\n"},
		{[]string{"get", "e"}, "$0\r\n\r\n"},
	}
	for _, tc := range cases {
		if got := reply(t, ctx, tc.args...); got != tc.want {
			t.Errorf("%v = %q, want %q", tc.args, got, tc.want)
		}
	}
}

// Options such as EX are outside the matrix, so they are an arity error.
func TestSetRejectsOptionsAndWrongArity(t *testing.T) {
	ctx := newCtx()
	mem := withAppender(ctx)
	cases := map[string][]string{
		"-ERR wrong number of arguments for 'set' command\r\n":    {"SET", "k", "v", "EX", "10"},
		"-ERR wrong number of arguments for 'get' command\r\n":    {"GET", "a", "b"},
		"-ERR wrong number of arguments for 'del' command\r\n":    {"DEL"},
		"-ERR wrong number of arguments for 'exists' command\r\n": {"EXISTS"},
	}
	for want, args := range cases {
		if got := reply(t, ctx, args...); got != want {
			t.Errorf("%v = %q, want %q", args, got, want)
		}
	}
	if ctx.Store.Exists("k") {
		t.Error("rejected SET still stored the key")
	}
	if len(mem.recs) != 0 {
		t.Errorf("rejected commands propagated %v", propagated(mem))
	}
}

// The stored value must own its bytes: the loop reuses the read buffer that
// the arguments alias, so a later frame must not rewrite an earlier SET.
func TestSetCopiesTheValueOutOfTheReadBuffer(t *testing.T) {
	ctx := newCtx()
	argv := dispatchArgs("SET", "k", "hello")
	Dispatch(ctx, argv)
	copy(argv[2], "XXXXX")
	if got := reply(t, ctx, "GET", "k"); got != "$5\r\nhello\r\n" {
		t.Fatalf("GET after buffer reuse = %q, want hello", got)
	}
}

// SET overwrites a key of another type and clears any TTL, as Redis does.
func TestSetOverwritesOtherTypesAndClearsTTL(t *testing.T) {
	ctx := newCtx()
	ctx.Store.Put("h", &store.Entry{
		Type:     store.THash,
		Val:      &store.HashVal{M: map[string][]byte{"f": []byte("v")}},
		ExpireAt: testNowMs + 60_000,
	})
	if got := reply(t, ctx, "GET", "h"); got != "-"+msgWrongType+"\r\n" {
		t.Fatalf("GET on a hash = %q, want WRONGTYPE", got)
	}
	reply(t, ctx, "SET", "h", "s")
	e, ok := ctx.Store.Lookup("h")
	if !ok || e.Type != store.TString {
		t.Fatalf("after SET, entry = %+v, want a string", e)
	}
	if e.ExpireAt != 0 || ctx.Store.ExpiringLen() != 0 {
		t.Fatalf("SET left a TTL behind: ExpireAt=%d ExpiringLen=%d", e.ExpireAt, ctx.Store.ExpiringLen())
	}
}

// GET must answer WRONGTYPE for every collection type, not just one.
func TestGetWrongTypeOnEveryCollection(t *testing.T) {
	ctx := newCtx()
	ctx.Store.Put("h", &store.Entry{Type: store.THash, Val: &store.HashVal{M: map[string][]byte{}}})
	ctx.Store.Put("s", &store.Entry{Type: store.TSet, Val: &store.SetVal{M: map[string]struct{}{}}})
	ctx.Store.Put("l", &store.Entry{Type: store.TList, Val: &store.ListVal{}})
	for _, k := range []string{"h", "s", "l"} {
		if got := reply(t, ctx, "GET", k); got != "-"+msgWrongType+"\r\n" {
			t.Errorf("GET %s = %q, want WRONGTYPE", k, got)
		}
	}
}

// Writes propagate their canonical form; DEL propagates only the keys that
// existed; reads and no-op DELs never consume a sequence number.
func TestStringCommandsPropagation(t *testing.T) {
	ctx := newCtx()
	mem := withAppender(ctx)
	reply(t, ctx, "SET", "a", "1")
	reply(t, ctx, "set", "b", "2")
	reply(t, ctx, "GET", "a")
	reply(t, ctx, "EXISTS", "a", "b", "zz")
	reply(t, ctx, "DEL", "zz")
	reply(t, ctx, "DEL", "a", "zz", "b", "a")

	want := [][]string{
		{"SET", "a", "1"},
		{"set", "b", "2"},
		{"DEL", "a", "b"},
	}
	if got := propagated(mem); !reflect.DeepEqual(got, want) {
		t.Fatalf("propagated %v, want %v", got, want)
	}
	if got := ctx.Srv.CurrentSeq(); got != 3 {
		t.Fatalf("CurrentSeq() = %d, want 3", got)
	}
}

// A key named twice counts twice in EXISTS but is removed once by DEL.
func TestExistsCountsDuplicatesDelDoesNot(t *testing.T) {
	ctx := newCtx()
	reply(t, ctx, "SET", "a", "1")
	if got := reply(t, ctx, "EXISTS", "a", "a", "b"); got != ":2\r\n" {
		t.Errorf("EXISTS a a b = %q, want :2", got)
	}
	if got := reply(t, ctx, "DEL", "a", "a"); got != ":1\r\n" {
		t.Errorf("DEL a a = %q, want :1", got)
	}
}

// An expired key is absent to GET and EXISTS and is not counted by DEL, and
// the DEL of an already-expired key does not propagate.
func TestExpiredKeyIsInvisibleToStringCommands(t *testing.T) {
	ctx, clock := newCtxWithClock()
	mem := withAppender(ctx)
	ctx.Store.Put("t", &store.Entry{
		Type: store.TString, Val: &store.StringVal{B: []byte("v")}, ExpireAt: testNowMs + 1000,
	})
	clock.Advance(time.Second)
	ctx.Now = clock.NowMs()
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"GET", "t"}, "$-1\r\n"},
		{[]string{"EXISTS", "t"}, ":0\r\n"},
		{[]string{"DEL", "t"}, ":0\r\n"},
	} {
		if got := reply(t, ctx, tc.args...); got != tc.want {
			t.Errorf("%v = %q, want %q", tc.args, got, tc.want)
		}
	}
	if len(mem.recs) != 0 {
		t.Fatalf("expired-key commands propagated %v", propagated(mem))
	}
}
