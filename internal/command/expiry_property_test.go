package command

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"testing"
	"time"
)

// The EXPIRE and PERSIST invariants from Proposal Section 7, checked under
// random interleaving against a reference model (T3.03).
//
// Every run uses seeds 1 to 10. To replay one failure, set
// RFS_EXPIRY_SEED to the seed printed in the failing subtest's name:
//
//	RFS_EXPIRY_SEED=7 go test ./internal/command -run TestExpiryProperty -v

const (
	propSteps = 1000
	propKeys  = 20
)

// modelKey is the reference model's view of one key: present or not, and
// its absolute expiry (0 for none). It applies the same rules as Redis.
type modelKey struct {
	present  bool
	expireAt int64
}

type expiryModel struct {
	now  int64
	keys map[string]modelKey
}

// live applies expiry to the model the way the store does on access.
func (m *expiryModel) live(k string) (modelKey, bool) {
	mk := m.keys[k]
	if !mk.present {
		return mk, false
	}
	if mk.expireAt != 0 && mk.expireAt <= m.now {
		delete(m.keys, k)
		return modelKey{}, false
	}
	return mk, true
}

func (m *expiryModel) ttl(k string) int64 {
	mk, ok := m.live(k)
	switch {
	case !ok:
		return -2
	case mk.expireAt == 0:
		return -1
	}
	return (mk.expireAt - m.now + 500) / 1000
}

func propSeeds(t *testing.T) []int64 {
	if s := os.Getenv("RFS_EXPIRY_SEED"); s != "" {
		seed, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			t.Fatalf("RFS_EXPIRY_SEED=%q is not an integer", s)
		}
		return []int64{seed}
	}
	return []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
}

func TestExpiryProperty(t *testing.T) {
	for _, seed := range propSeeds(t) {
		seed := seed
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			runExpiryProgram(t, seed)
		})
	}
}

func runExpiryProgram(t *testing.T, seed int64) {
	rng := rand.New(rand.NewSource(seed))
	ctx, clock := newCtxWithClock()
	mem := withAppender(ctx)
	model := &expiryModel{now: ctx.Now, keys: map[string]modelKey{}}

	names := make([]string, propKeys)
	for i := range names {
		names[i] = "k" + strconv.Itoa(i)
	}

	for step := 0; step < propSteps; step++ {
		k := names[rng.Intn(propKeys)]
		var args []string
		var want string
		switch op := rng.Intn(7); op {
		case 0: // SET
			args = []string{"SET", k, "v"}
			want = "+OK\r\n"
			model.keys[k] = modelKey{present: true}
		case 1: // EXPIRE with a TTL that may be negative, zero or short
			secs := int64(rng.Intn(8) - 2)
			args = []string{"EXPIRE", k, strconv.FormatInt(secs, 10)}
			if _, ok := model.live(k); !ok {
				want = ":0\r\n"
				break
			}
			want = ":1\r\n"
			if at := model.now + secs*1000; at <= model.now {
				delete(model.keys, k)
			} else {
				model.keys[k] = modelKey{present: true, expireAt: at}
			}
		case 2: // PERSIST
			args = []string{"PERSIST", k}
			mk, ok := model.live(k)
			if !ok || mk.expireAt == 0 {
				want = ":0\r\n"
				break
			}
			want = ":1\r\n"
			model.keys[k] = modelKey{present: true}
		case 3: // DEL
			args = []string{"DEL", k}
			if _, ok := model.live(k); ok {
				want = ":1\r\n"
				delete(model.keys, k)
			} else {
				want = ":0\r\n"
			}
		case 4: // TTL
			args = []string{"TTL", k}
			want = ":" + strconv.FormatInt(model.ttl(k), 10) + "\r\n"
		case 5: // advance the clock by up to 3 s
			d := time.Duration(rng.Intn(3000)) * time.Millisecond
			advance(ctx, clock, d)
			model.now += d.Milliseconds()
		case 6: // a loop tick: the active sweep must agree with the model
			ctx.Store.SweepExpired(sweepSample, time.Second)
		}
		if args != nil {
			if got := reply(t, ctx, args...); got != want {
				t.Fatalf("step %d: %v = %q, model says %q", step, args, got, want)
			}
		}
		for _, name := range names {
			_, inModel := model.live(name)
			exists := reply(t, ctx, "EXISTS", name) == ":1\r\n"
			if exists != inModel {
				t.Fatalf("step %d: EXISTS %s = %v, model says %v", step, name, exists, inModel)
			}
			wantTTL := ":" + strconv.FormatInt(model.ttl(name), 10) + "\r\n"
			if got := reply(t, ctx, "TTL", name); got != wantTTL {
				t.Fatalf("step %d: TTL %s = %q, model says %q", step, name, got, wantTTL)
			}
		}
	}

	// Whatever happened, nothing relative ever reached the log.
	for _, r := range mem.recs {
		switch r.args[0] {
		case "SET", "DEL", "PERSIST":
		case "PEXPIREAT":
			if at, err := strconv.ParseInt(r.args[2], 10, 64); err != nil || at <= testNowMs {
				t.Fatalf("seq %d: PEXPIREAT carries %q, not a future absolute time", r.seq, r.args[2])
			}
		default:
			t.Fatalf("seq %d: unexpected propagated command %v", r.seq, r.args)
		}
	}
}

// The explicit cases T3.03 names, each one an invariant on its own.
func TestExpiryExplicitInvariants(t *testing.T) {
	ctx := newCtx()
	steps := []struct {
		args []string
		want string
	}{
		// PERSIST on a key without a TTL returns 0.
		{[]string{"SET", "a", "v"}, "+OK\r\n"},
		{[]string{"PERSIST", "a"}, ":0\r\n"},
		// EXPIRE 0 deletes at once and TTL then returns -2.
		{[]string{"EXPIRE", "a", "0"}, ":1\r\n"},
		{[]string{"TTL", "a"}, ":-2\r\n"},
		{[]string{"EXISTS", "a"}, ":0\r\n"},
		// SET after EXPIRE clears the TTL.
		{[]string{"SET", "b", "v"}, "+OK\r\n"},
		{[]string{"EXPIRE", "b", "100"}, ":1\r\n"},
		{[]string{"SET", "b", "w"}, "+OK\r\n"},
		{[]string{"TTL", "b"}, ":-1\r\n"},
		// EXPIRE twice takes the later value, even when it is shorter.
		{[]string{"EXPIRE", "b", "100"}, ":1\r\n"},
		{[]string{"EXPIRE", "b", "10"}, ":1\r\n"},
		{[]string{"TTL", "b"}, ":10\r\n"},
	}
	for _, s := range steps {
		if got := reply(t, ctx, s.args...); got != s.want {
			t.Fatalf("%v = %q, want %q", s.args, got, s.want)
		}
	}
}
