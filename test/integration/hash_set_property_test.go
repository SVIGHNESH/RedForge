package integration

import (
	"fmt"
	"github.com/SVIGHNESH/RedForge/internal/testutil"
	"math/rand"
	"reflect"
	"sort"
	"strconv"
	"testing"
)

// collectionOps is how many random operations each seed runs.
const collectionOps = 5000

// collectionCheckEvery is how often the full state is compared with the model.
const collectionCheckEvery = 50

// Small field and member alphabets make overwrites, duplicate adds and
// removal of the last element frequent, which is where the invariants live.
const (
	hashFields = 8
	setMembers = 10
)

// Proposal Section 7: random HSET and HDEL interleavings match a map model,
// a hash with zero fields stops existing as a key, and every reply count is
// exactly what the model predicts. Replay one seed with
// RFS_PROPERTY_SEED=<seed>.
func TestHashProperty(t *testing.T) {
	for _, seed := range propertySeeds(t) {
		seed := seed
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			runHashProperty(t, seed)
		})
	}
}

func runHashProperty(t *testing.T, seed int64) {
	c := testutil.Start(t).Client(t)
	rng := rand.New(rand.NewSource(seed))
	model := map[string]string{}
	fail := func(op int, format string, a ...any) {
		t.Helper()
		t.Fatalf("seed %d, op %d: %s (replay with RFS_PROPERTY_SEED=%d)", seed, op, fmt.Sprintf(format, a...), seed)
	}

	for op := 1; op <= collectionOps; op++ {
		n := 1 + rng.Intn(3)
		if rng.Intn(2) == 0 {
			args := []string{"HSET", "h"}
			fresh := map[string]bool{}
			for i := 0; i < n; i++ {
				f := "f" + strconv.Itoa(rng.Intn(hashFields))
				v := strconv.Itoa(op) + "." + strconv.Itoa(i)
				args = append(args, f, v)
				if _, ok := model[f]; !ok {
					fresh[f] = true
				}
				model[f] = v
			}
			if got := c.MustInt(t, args...); got != int64(len(fresh)) {
				fail(op, "%v replied %d, model added %d new fields", args, got, len(fresh))
			}
		} else {
			args := []string{"HDEL", "h"}
			removed := 0
			for i := 0; i < n; i++ {
				f := "f" + strconv.Itoa(rng.Intn(hashFields))
				args = append(args, f)
				if _, ok := model[f]; ok {
					delete(model, f)
					removed++
				}
			}
			if got := c.MustInt(t, args...); got != int64(removed) {
				fail(op, "%v replied %d, model removed %d", args, got, removed)
			}
		}
		if op%collectionCheckEvery == 0 || op == collectionOps {
			checkHashAgainstModel(t, c, model, fail, op)
		}
	}
}

func checkHashAgainstModel(t *testing.T, c *testutil.Client, model map[string]string, fail func(int, string, ...any), op int) {
	flat := c.MustStrings(t, "HGETALL", "h")
	if len(flat)%2 != 0 {
		fail(op, "HGETALL returned an odd number of items: %v", flat)
	}
	got := map[string]string{}
	for i := 0; i < len(flat); i += 2 {
		if _, dup := got[flat[i]]; dup {
			fail(op, "HGETALL repeated field %q", flat[i])
		}
		got[flat[i]] = flat[i+1]
	}
	if !reflect.DeepEqual(got, model) {
		fail(op, "HGETALL = %v, model %v", got, model)
	}
	if n := c.MustInt(t, "HLEN", "h"); n != int64(len(model)) {
		fail(op, "HLEN = %d, model %d", n, len(model))
	}
	if e := c.MustInt(t, "EXISTS", "h"); (e == 1) != (len(model) > 0) {
		fail(op, "EXISTS = %d with %d fields in the model", e, len(model))
	}
}

// Proposal Section 7: random SADD and SREM interleavings on two sets match a
// map model; SADD of an existing member returns 0 and leaves the cardinality
// unchanged; an empty set stops existing; SINTER equals the model
// intersection. Replay one seed with RFS_PROPERTY_SEED=<seed>.
func TestSetProperty(t *testing.T) {
	for _, seed := range propertySeeds(t) {
		seed := seed
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			runSetProperty(t, seed)
		})
	}
}

func runSetProperty(t *testing.T, seed int64) {
	c := testutil.Start(t).Client(t)
	rng := rand.New(rand.NewSource(seed))
	models := map[string]map[string]bool{"s1": {}, "s2": {}}
	keys := []string{"s1", "s2"}
	fail := func(op int, format string, a ...any) {
		t.Helper()
		t.Fatalf("seed %d, op %d: %s (replay with RFS_PROPERTY_SEED=%d)", seed, op, fmt.Sprintf(format, a...), seed)
	}

	for op := 1; op <= collectionOps; op++ {
		key := keys[rng.Intn(len(keys))]
		model := models[key]
		n := 1 + rng.Intn(3)
		if rng.Intn(2) == 0 {
			args := []string{"SADD", key}
			added := 0
			for i := 0; i < n; i++ {
				m := "m" + strconv.Itoa(rng.Intn(setMembers))
				args = append(args, m)
				if !model[m] {
					model[m] = true
					added++
				}
			}
			if got := c.MustInt(t, args...); got != int64(added) {
				fail(op, "%v replied %d, model added %d", args, got, added)
			}
		} else {
			args := []string{"SREM", key}
			removed := 0
			for i := 0; i < n; i++ {
				m := "m" + strconv.Itoa(rng.Intn(setMembers))
				args = append(args, m)
				if model[m] {
					delete(model, m)
					removed++
				}
			}
			if got := c.MustInt(t, args...); got != int64(removed) {
				fail(op, "%v replied %d, model removed %d", args, got, removed)
			}
		}
		if op%collectionCheckEvery == 0 || op == collectionOps {
			for _, k := range keys {
				checkSetAgainstModel(t, c, k, models[k], fail, op)
			}
			checkDuplicateAdd(t, c, keys, models, fail, op)
			want := []string{}
			for m := range models["s1"] {
				if models["s2"][m] {
					want = append(want, m)
				}
			}
			sort.Strings(want)
			got := c.MustStrings(t, "SINTER", "s1", "s2")
			sort.Strings(got)
			if !reflect.DeepEqual(got, want) {
				fail(op, "SINTER s1 s2 = %v, model %v", got, want)
			}
		}
	}
}

func checkSetAgainstModel(t *testing.T, c *testutil.Client, key string, model map[string]bool, fail func(int, string, ...any), op int) {
	got := c.MustStrings(t, "SMEMBERS", key)
	sort.Strings(got)
	want := make([]string, 0, len(model))
	for m := range model {
		want = append(want, m)
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		fail(op, "SMEMBERS %s = %v, model %v", key, got, want)
	}
	if e := c.MustInt(t, "EXISTS", key); (e == 1) != (len(model) > 0) {
		fail(op, "EXISTS %s = %d with %d members in the model", key, e, len(model))
	}
	for m := range model {
		if c.MustInt(t, "SISMEMBER", key, m) != 1 {
			fail(op, "SISMEMBER %s %s = 0, model has it", key, m)
		}
	}
}

// checkDuplicateAdd re-adds a member that is already present: SADD must reply
// 0 and the cardinality must not move. The model is unchanged by definition.
func checkDuplicateAdd(t *testing.T, c *testutil.Client, keys []string, models map[string]map[string]bool, fail func(int, string, ...any), op int) {
	for _, k := range keys {
		for m := range models[k] {
			before := len(c.MustStrings(t, "SMEMBERS", k))
			if got := c.MustInt(t, "SADD", k, m, m); got != 0 {
				fail(op, "SADD %s %s %s of an existing member replied %d, want 0", k, m, m, got)
			}
			if after := len(c.MustStrings(t, "SMEMBERS", k)); after != before {
				fail(op, "duplicate SADD changed %s cardinality from %d to %d", k, before, after)
			}
			break
		}
	}
}
