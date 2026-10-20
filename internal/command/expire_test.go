package command

import (
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/store"
)

func expiredKeysOf(ctx *Ctx) int64 { return ctx.Store.(store.Counters).ExpiredKeys() }

// advance moves the fake clock and refreshes ctx.Now, as the loop does
// before the next command.
func advance(ctx *Ctx, clock interface{ Advance(time.Duration) }, d time.Duration) {
	clock.Advance(d)
	ctx.Now += d.Milliseconds()
}

func TestExpireTTLPersistSession(t *testing.T) {
	ctx := newCtx()
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"TTL", "s"}, ":-2\r\n"},
		{[]string{"SET", "s", "1"}, "+OK\r\n"},
		{[]string{"TTL", "s"}, ":-1\r\n"},
		{[]string{"EXPIRE", "s", "100"}, ":1\r\n"},
		{[]string{"TTL", "s"}, ":100\r\n"},
		{[]string{"PERSIST", "s"}, ":1\r\n"},
		{[]string{"TTL", "s"}, ":-1\r\n"},
		{[]string{"PERSIST", "s"}, ":0\r\n"},
		{[]string{"PERSIST", "nokey"}, ":0\r\n"},
		{[]string{"EXPIRE", "nokey", "10"}, ":0\r\n"},
		{[]string{"EXPIRE", "s", "abc"}, "-" + msgNotInt + "\r\n"},
		{[]string{"EXPIRE", "s", "+5"}, "-" + msgNotInt + "\r\n"},
		{[]string{"EXPIRE", "s", "9223372036854775807"}, "-ERR invalid expire time in 'expire' command\r\n"},
		{[]string{"EXPIRE", "s", "-9223372036854775808"}, "-ERR invalid expire time in 'expire' command\r\n"},
		{[]string{"PEXPIREAT", "s", "x"}, "-" + msgNotInt + "\r\n"},
		{[]string{"TTL", "s"}, ":-1\r\n"},
	}
	for _, tc := range cases {
		if got := reply(t, ctx, tc.args...); got != tc.want {
			t.Errorf("%v = %q, want %q", tc.args, got, tc.want)
		}
	}
}

// A key with EXPIRE 1 is present 999 ms later and gone at 1000 ms.
func TestExpireBoundaryWithFakeClock(t *testing.T) {
	ctx, clock := newCtxWithClock()
	reply(t, ctx, "SET", "k", "v")
	reply(t, ctx, "EXPIRE", "k", "1")
	advance(ctx, clock, 999*time.Millisecond)
	if got := reply(t, ctx, "GET", "k"); got != "$1\r\nv\r\n" {
		t.Fatalf("GET 1 ms before expiry = %q, want v", got)
	}
	advance(ctx, clock, time.Millisecond)
	if got := reply(t, ctx, "GET", "k"); got != "$-1\r\n" {
		t.Fatalf("GET at expiry = %q, want nil", got)
	}
	if got := reply(t, ctx, "TTL", "k"); got != ":-2\r\n" {
		t.Fatalf("TTL after expiry = %q, want -2", got)
	}
}

// TTL rounds to the nearest second, as Redis does: these rows were checked
// against a reference server with PEXPIRE.
func TestTTLRounding(t *testing.T) {
	cases := []struct {
		remainingMs int64
		want        string
	}{
		{1400, ":1\r\n"},
		{1500, ":2\r\n"},
		{1600, ":2\r\n"},
		{400, ":0\r\n"},
		{100_000, ":100\r\n"},
		{99_499, ":99\r\n"},
	}
	for _, tc := range cases {
		ctx := newCtx()
		reply(t, ctx, "SET", "k", "v")
		reply(t, ctx, "PEXPIREAT", "k", strconv.FormatInt(ctx.Now+tc.remainingMs, 10))
		if got := reply(t, ctx, "TTL", "k"); got != tc.want {
			t.Errorf("TTL with %d ms left = %q, want %q", tc.remainingMs, got, tc.want)
		}
	}
}

// The log must only ever see the absolute form: EXPIRE k 10 propagates
// PEXPIREAT k <now+10000>, never EXPIRE (Proposal Section 6.3).
func TestExpirePropagatesAbsolutePexpireat(t *testing.T) {
	ctx := newCtx()
	mem := withAppender(ctx)
	reply(t, ctx, "SET", "k", "v")
	reply(t, ctx, "EXPIRE", "k", "10")
	reply(t, ctx, "pexpireat", "k", strconv.FormatInt(testNowMs+20_000, 10))
	want := [][]string{
		{"SET", "k", "v"},
		{"PEXPIREAT", "k", strconv.FormatInt(testNowMs+10_000, 10)},
		{"PEXPIREAT", "k", strconv.FormatInt(testNowMs+20_000, 10)},
	}
	if got := propagated(mem); !reflect.DeepEqual(got, want) {
		t.Fatalf("propagated %v, want %v", got, want)
	}
}

// A non-positive TTL, or an absolute time already past, deletes the key at
// once and propagates DEL, not an expiry.
func TestExpireInThePastDeletesAndPropagatesDel(t *testing.T) {
	ctx := newCtx()
	mem := withAppender(ctx)
	for _, args := range [][]string{
		{"EXPIRE", "a", "0"},
		{"EXPIRE", "b", "-10"},
		{"PEXPIREAT", "c", "1"},
		{"PEXPIREAT", "d", strconv.FormatInt(testNowMs, 10)},
	} {
		reply(t, ctx, "SET", args[1], "v")
		mem.recs = nil
		if got := reply(t, ctx, args...); got != ":1\r\n" {
			t.Errorf("%v = %q, want :1", args, got)
		}
		if got := reply(t, ctx, "EXISTS", args[1]); got != ":0\r\n" {
			t.Errorf("after %v the key still exists", args)
		}
		want := [][]string{{"DEL", args[1]}}
		if got := propagated(mem); !reflect.DeepEqual(got, want) {
			t.Errorf("%v propagated %v, want %v", args, got, want)
		}
	}
	if ctx.Store.ExpiringLen() != 0 {
		t.Fatalf("ExpiringLen = %d, want 0", ctx.Store.ExpiringLen())
	}
}

// Every no-op or failed expiry command stays out of the log: PERSIST without
// a TTL, EXPIRE on a missing key, bad arguments, and TTL itself.
func TestExpiryNoOpsAndReadsDoNotPropagate(t *testing.T) {
	ctx := newCtx()
	reply(t, ctx, "SET", "k", "v")
	mem := withAppender(ctx)
	for _, args := range [][]string{
		{"PERSIST", "k"},
		{"PERSIST", "missing"},
		{"EXPIRE", "missing", "10"},
		{"EXPIRE", "missing", "0"},
		{"PEXPIREAT", "missing", "99999999999999"},
		{"EXPIRE", "k", "abc"},
		{"EXPIRE", "k", "9223372036854775807"},
		{"TTL", "k"},
	} {
		reply(t, ctx, args...)
	}
	if len(mem.recs) != 0 || ctx.Srv.CurrentSeq() != 1 {
		t.Fatalf("no-op expiry commands propagated %v (seq %d, want 1)", propagated(mem), ctx.Srv.CurrentSeq())
	}
}

// PERSIST propagates itself only when it removed a TTL.
func TestPersistPropagatesOnlyWhenItRemovedATTL(t *testing.T) {
	ctx := newCtx()
	reply(t, ctx, "SET", "k", "v")
	reply(t, ctx, "EXPIRE", "k", "100")
	mem := withAppender(ctx)
	reply(t, ctx, "PERSIST", "k")
	reply(t, ctx, "PERSIST", "k")
	if got, want := propagated(mem), [][]string{{"PERSIST", "k"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("propagated %v, want %v", got, want)
	}
}

// SET after EXPIRE clears the TTL; EXPIRE twice keeps the later value.
func TestSetClearsTTLAndSecondExpireWins(t *testing.T) {
	ctx := newCtx()
	reply(t, ctx, "SET", "k", "v")
	reply(t, ctx, "EXPIRE", "k", "100")
	reply(t, ctx, "SET", "k", "w")
	if got := reply(t, ctx, "TTL", "k"); got != ":-1\r\n" {
		t.Fatalf("TTL after SET = %q, want -1", got)
	}
	reply(t, ctx, "EXPIRE", "k", "100")
	reply(t, ctx, "EXPIRE", "k", "50")
	if got := reply(t, ctx, "TTL", "k"); got != ":50\r\n" {
		t.Fatalf("TTL after EXPIRE 100 then 50 = %q, want 50", got)
	}
}

// Passive expiry is counted, and the deletion it performs is never logged:
// replay and replicas drop the key from the absolute timestamp themselves.
func TestPassiveExpiryIsCountedButNotPropagated(t *testing.T) {
	ctx, clock := newCtxWithClock()
	reply(t, ctx, "SET", "k", "v")
	reply(t, ctx, "EXPIRE", "k", "1")
	mem := withAppender(ctx)
	advance(ctx, clock, 2*time.Second)
	reply(t, ctx, "GET", "k")
	if len(mem.recs) != 0 {
		t.Fatalf("passive expiry propagated %v", propagated(mem))
	}
	if got := expiredKeysOf(ctx); got != 1 {
		t.Fatalf("expired keys = %d, want 1", got)
	}
}
