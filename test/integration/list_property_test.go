package integration

import (
	"fmt"
	"github.com/SVIGHNESH/RedForge/internal/testutil"
	"math/rand"
	"reflect"
	"strconv"
	"testing"

	"github.com/SVIGHNESH/RedForge/internal/resp"
)

// listOps is how many random operations each seed runs (T4.03 step 2).
const listOps = 5000

// listCheckEvery is how often the full list is compared with the model.
const listCheckEvery = 50

// Proposal Section 7: any interleaving of pushes and pops at both ends leaves
// length and order exactly as a slice reference model predicts, and the key
// exists exactly when the model is non-empty. Every reply is also checked
// against the model as it arrives, so a wrong pop is caught on the spot.
// Replay one seed with RFS_PROPERTY_SEED=<seed>.
func TestListMixedEndProperty(t *testing.T) {
	for _, seed := range propertySeeds(t) {
		seed := seed
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			runListProperty(t, seed)
		})
	}
}

func runListProperty(t *testing.T, seed int64) {
	c := testutil.Start(t).Client(t)
	rng := rand.New(rand.NewSource(seed))
	var model []string
	fail := func(op int, format string, a ...any) {
		t.Helper()
		t.Fatalf("seed %d, op %d: %s (replay with RFS_PROPERTY_SEED=%d)", seed, op, fmt.Sprintf(format, a...), seed)
	}

	for op := 1; op <= listOps; op++ {
		switch rng.Intn(4) {
		case 0, 1:
			cmd := "LPUSH"
			if rng.Intn(2) == 1 {
				cmd = "RPUSH"
			}
			n := 1 + rng.Intn(3)
			args := []string{cmd, "l"}
			for i := 0; i < n; i++ {
				v := strconv.Itoa(op) + "." + strconv.Itoa(i)
				args = append(args, v)
				if cmd == "LPUSH" {
					model = append([]string{v}, model...)
				} else {
					model = append(model, v)
				}
			}
			if got := c.MustInt(t, args...); got != int64(len(model)) {
				fail(op, "%v replied %d, model length %d", args, got, len(model))
			}
		case 2, 3:
			cmd := "LPOP"
			if rng.Intn(2) == 1 {
				cmd = "RPOP"
			}
			v := c.MustDo(t, cmd, "l")
			if len(model) == 0 {
				if v.Kind != resp.KindBulk || !v.Null {
					fail(op, "%s on an empty list replied %s, want nil", cmd, testutil.Show(v))
				}
				continue
			}
			var want string
			if cmd == "LPOP" {
				want, model = model[0], model[1:]
			} else {
				want, model = model[len(model)-1], model[:len(model)-1]
			}
			if v.Kind != resp.KindBulk || v.Null || string(v.Str) != want {
				fail(op, "%s replied %s, model says %q", cmd, testutil.Show(v), want)
			}
		}
		if op%listCheckEvery == 0 {
			checkListAgainstModel(t, c, model, fail, op)
		}
	}
	checkListAgainstModel(t, c, model, fail, listOps)
}

func checkListAgainstModel(t *testing.T, c *testutil.Client, model []string, fail func(int, string, ...any), op int) {
	t.Helper()
	if got := c.MustInt(t, "LLEN", "l"); got != int64(len(model)) {
		fail(op, "LLEN = %d, model length %d", got, len(model))
	}
	got := c.MustStrings(t, "LRANGE", "l", "0", "-1")
	want := model
	if want == nil {
		want = []string{}
	}
	if !reflect.DeepEqual(got, want) {
		fail(op, "LRANGE = %v, model %v", got, want)
	}
	exists := c.MustInt(t, "EXISTS", "l")
	if (exists == 1) != (len(model) > 0) {
		fail(op, "EXISTS = %d with model length %d", exists, len(model))
	}
}
