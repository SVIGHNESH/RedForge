package command

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/resp"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

// keysOf runs KEYS pattern and returns the result sorted, because Redis does
// not order KEYS output and the tests must compare it as a set.
func keysOf(t *testing.T, ctx *Ctx, pattern string) []string {
	t.Helper()
	raw := Dispatch(ctx, dispatchArgs("KEYS", pattern)).AppendTo(nil)
	items, n, err := resp.ParseCommand(raw)
	if err != nil || n != len(raw) {
		t.Fatalf("KEYS %q reply %q did not parse as an array of bulks: %v", pattern, raw, err)
	}
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = string(it)
	}
	sort.Strings(out)
	return out
}

func TestKeysMatchesGlobPatterns(t *testing.T) {
	ctx := newCtx()
	for _, k := range []string{"user:1", "user:2", "user:10", "order:1", "aser:1"} {
		reply(t, ctx, "SET", k, "v")
	}
	cases := []struct {
		pattern string
		want    []string
	}{
		{"*", []string{"aser:1", "order:1", "user:1", "user:10", "user:2"}},
		{"user:*", []string{"user:1", "user:10", "user:2"}},
		{"?ser:1", []string{"aser:1", "user:1"}},
		{"user:[12]", []string{"user:1", "user:2"}},
		{"nomatch*", []string{}},
	}
	for _, tc := range cases {
		if got := keysOf(t, ctx, tc.pattern); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("KEYS %q = %v, want %v", tc.pattern, got, tc.want)
		}
	}
}

func TestKeysOnEmptyKeyspaceIsAnEmptyArray(t *testing.T) {
	ctx := newCtx()
	if got := reply(t, ctx, "KEYS", "*"); got != "*0\r\n" {
		t.Fatalf("KEYS * on empty store = %q, want *0", got)
	}
}

// An expired key is never listed, and walking past it reclaims it, so it is
// gone from the store afterwards rather than just hidden.
func TestKeysExcludesAndReclaimsExpiredKeys(t *testing.T) {
	ctx, clock := newCtxWithClock()
	reply(t, ctx, "SET", "live", "v")
	ctx.Store.Put("dead", &store.Entry{
		Type: store.TString, Val: &store.StringVal{B: []byte("v")}, ExpireAt: testNowMs + 10,
	})
	clock.Advance(10 * time.Millisecond)
	ctx.Now = clock.NowMs()

	if got := keysOf(t, ctx, "*"); !reflect.DeepEqual(got, []string{"live"}) {
		t.Fatalf("KEYS * = %v, want [live]", got)
	}
	if ctx.Store.Len() != 1 || ctx.Store.ExpiringLen() != 0 {
		t.Fatalf("expired key not reclaimed: Len=%d ExpiringLen=%d", ctx.Store.Len(), ctx.Store.ExpiringLen())
	}
}

// KEYS is a read: it must never propagate, even when it reclaims keys.
func TestKeysDoesNotPropagate(t *testing.T) {
	ctx := newCtx()
	mem := withAppender(ctx)
	ctx.Store.Put("a", &store.Entry{Type: store.TString, Val: &store.StringVal{B: []byte("v")}})
	reply(t, ctx, "KEYS", "*")
	if len(mem.recs) != 0 || ctx.Srv.CurrentSeq() != 0 {
		t.Fatalf("KEYS propagated %v", propagated(mem))
	}
}

// The INFO counter for reply size must equal the bytes actually sent.
func TestKeysRecordsReplyBytes(t *testing.T) {
	ctx := newCtx()
	for _, k := range []string{"a", "bb", "ccc"} {
		reply(t, ctx, "SET", k, "v")
	}
	got := reply(t, ctx, "KEYS", "*")
	if ctx.Srv.Stats.KeysLastReplyBytes != int64(len(got)) {
		t.Fatalf("KeysLastReplyBytes = %d, reply was %d bytes", ctx.Srv.Stats.KeysLastReplyBytes, len(got))
	}
	if ctx.Srv.Stats.KeysLastIterUs < 0 {
		t.Fatalf("KeysLastIterUs = %d, want >= 0", ctx.Srv.Stats.KeysLastIterUs)
	}
}
