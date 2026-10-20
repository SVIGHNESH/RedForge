package command

import (
	"reflect"
	"testing"

	"github.com/SVIGHNESH/RedForge/internal/store"
)

// Every row was checked against a reference redis-server: SET k <stored>
// then INCR k. The "missing" row runs INCR on an absent key.
func TestIncrMatchesRedisIntegerRules(t *testing.T) {
	notInt := "-" + msgNotInt + "\r\n"
	cases := []struct {
		stored string
		want   string
	}{
		{"0", ":1\r\n"},
		{"-5", ":-4\r\n"},
		{"41", ":42\r\n"},
		{"9223372036854775806", ":9223372036854775807\r\n"},
		{"-9223372036854775808", ":-9223372036854775807\r\n"},
		{"9223372036854775807", "-" + msgOverflow + "\r\n"},
		{"007", notInt},
		{"00", notInt},
		{"+1", notInt},
		{" 1", notInt},
		{"1 ", notInt},
		{"-0", notInt},
		{"-", notInt},
		{"", notInt},
		{"abc", notInt},
		{"1.5", notInt},
		{"99999999999999999999", notInt},
		{"9223372036854775808", notInt},
	}
	for _, tc := range cases {
		ctx := newCtx()
		reply(t, ctx, "SET", "k", tc.stored)
		if got := reply(t, ctx, "INCR", "k"); got != tc.want {
			t.Errorf("SET k %q; INCR k = %q, want %q", tc.stored, got, tc.want)
		}
	}
	ctx := newCtx()
	if got := reply(t, ctx, "INCR", "missing"); got != ":1\r\n" {
		t.Errorf("INCR on a missing key = %q, want :1", got)
	}
	if got := reply(t, ctx, "GET", "missing"); got != "$1\r\n1\r\n" {
		t.Errorf("GET after INCR = %q, want \"1\"", got)
	}
}

func TestIncrOnACollectionIsWrongType(t *testing.T) {
	ctx := newCtx()
	ctx.Store.Put("h", &store.Entry{Type: store.THash, Val: &store.HashVal{M: map[string][]byte{}}})
	if got := reply(t, ctx, "INCR", "h"); got != "-"+msgWrongType+"\r\n" {
		t.Fatalf("INCR on a hash = %q, want WRONGTYPE", got)
	}
}

// INCR propagates INCR k, never SET k <value>, and a failed INCR consumes no
// sequence number and leaves the stored value untouched.
func TestIncrPropagationAndFailureAtomicity(t *testing.T) {
	ctx := newCtx()
	mem := withAppender(ctx)
	reply(t, ctx, "INCR", "c")
	reply(t, ctx, "incr", "c")
	reply(t, ctx, "SET", "bad", "x")
	reply(t, ctx, "INCR", "bad")
	reply(t, ctx, "SET", "max", "9223372036854775807")
	reply(t, ctx, "INCR", "max")

	want := [][]string{
		{"INCR", "c"},
		{"incr", "c"},
		{"SET", "bad", "x"},
		{"SET", "max", "9223372036854775807"},
	}
	if got := propagated(mem); !reflect.DeepEqual(got, want) {
		t.Fatalf("propagated %v, want %v", got, want)
	}
	if got := reply(t, ctx, "GET", "max"); got != "$19\r\n9223372036854775807\r\n" {
		t.Fatalf("overflowing INCR changed the value: GET max = %q", got)
	}
}

// INCR must keep the key's TTL, as Redis does, and must store a new value
// instead of rewriting the old bytes, which a snapshot may still share.
func TestIncrKeepsTTLAndNeverMutatesTheOldValue(t *testing.T) {
	ctx := newCtx()
	expireAt := int64(testNowMs + 100_000)
	old := &store.StringVal{B: []byte("5")}
	ctx.Store.Put("t", &store.Entry{Type: store.TString, Val: old, ExpireAt: expireAt})

	if got := reply(t, ctx, "INCR", "t"); got != ":6\r\n" {
		t.Fatalf("INCR t = %q, want :6", got)
	}
	e, ok := ctx.Store.Lookup("t")
	if !ok || e.ExpireAt != expireAt {
		t.Fatalf("INCR dropped the TTL: entry %+v", e)
	}
	if string(old.B) != "5" {
		t.Fatalf("INCR rewrote the old value in place: %q", old.B)
	}
}

// Many INCRs on one key land on exactly the sum, the single-threaded
// invariant the lost-update test (T3.04) later checks over the network.
func TestIncrSequenceIsExact(t *testing.T) {
	ctx := newCtx()
	for i := 0; i < 1000; i++ {
		reply(t, ctx, "INCR", "n")
	}
	if got := reply(t, ctx, "GET", "n"); got != "$4\r\n1000\r\n" {
		t.Fatalf("after 1000 INCRs GET n = %q, want 1000", got)
	}
	if ctx.Srv.CurrentSeq() != 1000 {
		t.Fatalf("CurrentSeq() = %d, want 1000", ctx.Srv.CurrentSeq())
	}
}
