package integration

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/SVIGHNESH/RedForge/internal/testutil"
)

// ccClients is how many concurrent clients the concurrency tests open.
const ccClients = 100

// incrsPerClient is N in CC3: each client issues N INCRs. -short drops it so
// the test still runs, just smaller.
func incrsPerClient() int {
	if testing.Short() {
		return 100
	}
	return 1000
}

// CC3, synopsis success criterion 4: 100 clients each sending N sequential
// INCRs on one shared key must leave exactly 100 times N. A lost update,
// which is what breaking single-threaded command atomicity would cause,
// leaves less.
func TestSharedKeyIncrLosesNoUpdates(t *testing.T) {
	p := testutil.Start(t)
	n := incrsPerClient()
	runClients(t, p, func(c *testutil.Client) error {
		for i := 0; i < n; i++ {
			v, err := c.Do("INCR", "shared")
			if err != nil {
				return err
			}
			if v.Int < 1 {
				return fmt.Errorf("INCR replied %s", testutil.Show(v))
			}
		}
		return nil
	})
	want := strconv.Itoa(ccClients * n)
	if got := p.Client(t).MustBulk(t, "GET", "shared"); got != want {
		t.Fatalf("shared = %s after %d clients x %d INCR, want %s", got, ccClients, n, want)
	}
}

// The same invariant with each client pipelining its INCRs in batches of 100,
// so many commands from many clients sit in the loop's buffers at once.
func TestSharedKeyPipelinedIncrLosesNoUpdates(t *testing.T) {
	p := testutil.Start(t)
	n := incrsPerClient()
	const batch = 100
	runClients(t, p, func(c *testutil.Client) error {
		cmds := make([][]string, batch)
		for i := range cmds {
			cmds[i] = []string{"INCR", "shared"}
		}
		for sent := 0; sent < n; sent += batch {
			replies, err := c.Pipeline(cmds)
			if err != nil {
				return err
			}
			// Within one client's pipeline the counter only ever grows.
			for i := 1; i < len(replies); i++ {
				if replies[i].Int <= replies[i-1].Int {
					return fmt.Errorf("pipelined INCR replies went %d then %d", replies[i-1].Int, replies[i].Int)
				}
			}
		}
		return nil
	})
	want := strconv.Itoa(ccClients * n)
	if got := p.Client(t).MustBulk(t, "GET", "shared"); got != want {
		t.Fatalf("shared = %s after %d clients x %d pipelined INCR, want %s", got, ccClients, n, want)
	}
}

// runClients runs fn on ccClients concurrent connections, each with its own
// testutil.Client, and fails the test on the first error.
func runClients(t *testing.T, p *testutil.ServerProcess, fn func(c *testutil.Client) error) {
	t.Helper()
	runN(t, p, ccClients, func(_ int, c *testutil.Client) error { return fn(c) })
}
