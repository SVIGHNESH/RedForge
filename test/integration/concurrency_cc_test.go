package integration

import (
	"fmt"
	"strconv"
	"sync"
	"testing"

	"github.com/SVIGHNESH/RedForge/internal/resp"
	"github.com/SVIGHNESH/RedForge/internal/testutil"
)

// CC1: 100 clients write disjoint keys concurrently; afterwards every one of
// the 10000 writes is present, KEYS sees exactly them, and INFO agrees.
func TestDisjointConcurrentWritesAllLand(t *testing.T) {
	p := testutil.Start(t)
	const keysPerClient = 100
	var wg sync.WaitGroup
	errs := make(chan error, ccClients)
	for i := 0; i < ccClients; i++ {
		c := p.Client(t)
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < keysPerClient; j++ {
				v, err := c.Do("SET", fmt.Sprintf("c%d:k%d", i, j), fmt.Sprintf("%d-%d", i, j))
				if err != nil {
					errs <- err
					return
				}
				if string(v.Str) != "OK" {
					errs <- fmt.Errorf("client %d SET %d replied %s", i, j, testutil.Show(v))
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	check := p.Client(t)
	cmds := make([][]string, 0, ccClients*keysPerClient)
	for i := 0; i < ccClients; i++ {
		for j := 0; j < keysPerClient; j++ {
			cmds = append(cmds, []string{"GET", fmt.Sprintf("c%d:k%d", i, j)})
		}
	}
	replies, err := check.Pipeline(cmds)
	if err != nil {
		t.Fatal(err)
	}
	for n, v := range replies {
		i, j := n/keysPerClient, n%keysPerClient
		if want := fmt.Sprintf("%d-%d", i, j); v.Null || string(v.Str) != want {
			t.Fatalf("GET c%d:k%d = %s, want %q", i, j, testutil.Show(v), want)
		}
	}
	if got := len(check.MustStrings(t, "KEYS", "c*")); got != ccClients*keysPerClient {
		t.Fatalf("KEYS c* returned %d keys, want %d", got, ccClients*keysPerClient)
	}
	if db0 := testutil.MustInfo(t, check, "keyspace")["db0"]; db0 != "keys=10000,expires=0" {
		t.Fatalf("INFO db0 = %q, want keys=10000,expires=0", db0)
	}
}

// CC2: 10 clients each pipeline 1000 INCRs on their own key in one write.
// Replies must come back in issue order, 1 to 1000, and the final values must
// be 1000, while the other clients' pipelines are interleaved by the loop.
func TestPipelinedIncrRepliesKeepIssueOrder(t *testing.T) {
	p := testutil.Start(t)
	const clients, depth = 10, 1000
	runN(t, p, clients, func(i int, c *testutil.Client) error {
		key := "own" + strconv.Itoa(i)
		cmds := make([][]string, depth)
		for j := range cmds {
			cmds[j] = []string{"INCR", key}
		}
		replies, err := c.Pipeline(cmds)
		if err != nil {
			return err
		}
		for j, v := range replies {
			if v.Kind != resp.KindInteger || v.Int != int64(j+1) {
				return fmt.Errorf("client %d reply %d = %s, want %d", i, j, testutil.Show(v), j+1)
			}
		}
		return nil
	})
	c := p.Client(t)
	for i := 0; i < clients; i++ {
		if got := c.MustBulk(t, "GET", "own"+strconv.Itoa(i)); got != strconv.Itoa(depth) {
			t.Fatalf("own%d = %s, want %d", i, got, depth)
		}
	}
}

// CC2 variant: a pipeline mixing SET, GET and INCR gets its replies in issue
// order with the right type for each command.
func TestMixedPipelineRepliesKeepOrderAndType(t *testing.T) {
	p := testutil.Start(t)
	const clients, rounds = 10, 300
	runN(t, p, clients, func(i int, c *testutil.Client) error {
		x, y := "x"+strconv.Itoa(i), "y"+strconv.Itoa(i)
		var cmds [][]string
		for r := 0; r < rounds; r++ {
			v := strconv.Itoa(r)
			cmds = append(cmds, []string{"SET", x, v}, []string{"GET", x}, []string{"INCR", y})
		}
		replies, err := c.Pipeline(cmds)
		if err != nil {
			return err
		}
		for r := 0; r < rounds; r++ {
			set, get, incr := replies[3*r], replies[3*r+1], replies[3*r+2]
			if set.Kind != resp.KindSimpleString || string(set.Str) != "OK" {
				return fmt.Errorf("client %d round %d SET = %s", i, r, testutil.Show(set))
			}
			if get.Kind != resp.KindBulk || string(get.Str) != strconv.Itoa(r) {
				return fmt.Errorf("client %d round %d GET = %s, want %d", i, r, testutil.Show(get), r)
			}
			if incr.Kind != resp.KindInteger || incr.Int != int64(r+1) {
				return fmt.Errorf("client %d round %d INCR = %s, want %d", i, r, testutil.Show(incr), r+1)
			}
		}
		return nil
	})
}

// runN runs fn concurrently on n clients, fn receiving the client's index,
// and fails the test on the first error.
func runN(t *testing.T, p *testutil.ServerProcess, n int, fn func(i int, c *testutil.Client) error) {
	t.Helper()
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		c := p.Client(t)
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := fn(i, c); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}
