package integration

import (
	"strconv"
	"testing"

	"github.com/SVIGHNESH/RedForge/internal/testutil"
)

// M2: 100 open client connections must not add a single goroutine to the
// server. The baseline is sampled after startup rather than asserted as an
// absolute number, because the server legitimately runs a fixed set of
// background goroutines (the everysec fsync goroutine, for one) that do not
// scale with clients.
func TestHundredConnectionsAddNoGoroutines(t *testing.T) {
	p := testutil.Start(t)
	first := p.Client(t)
	base := goroutines(t, first)

	clients := make([]*testutil.Client, 100)
	for i := range clients {
		clients[i] = p.Client(t)
		if v := clients[i].MustDo(t, "PING"); string(v.Str) != "PONG" {
			t.Fatalf("client %d PING = %s", i, testutil.Show(v))
		}
	}
	if n := testutil.MustInfo(t, first, "clients")["connected_clients"]; n != "101" {
		t.Fatalf("connected_clients = %s, want 101", n)
	}
	if got := goroutines(t, first); got != base {
		t.Fatalf("server goroutines went from %d to %d with 100 connections open", base, got)
	}

	// Every connection is still served, so none of them is parked.
	for i, c := range clients {
		if v := c.MustDo(t, "PING"); string(v.Str) != "PONG" {
			t.Fatalf("client %d second PING = %s", i, testutil.Show(v))
		}
	}
	if got := goroutines(t, first); got != base {
		t.Fatalf("server goroutines went from %d to %d after serving 100 clients", base, got)
	}
}

func goroutines(t *testing.T, c *testutil.Client) int {
	t.Helper()
	raw := testutil.MustInfo(t, c, "server")["goroutines"]
	n, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("INFO goroutines = %q, not a number", raw)
	}
	return n
}
