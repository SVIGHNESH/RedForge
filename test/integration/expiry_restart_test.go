package integration

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/testutil"
)

// The four expiry tests of MASTER-PLAN MS7, PROPOSAL 6.3 as executable checks.
// Wall-clock sleeps are the point here, so none of them uses a fake clock.
//
//   - EX1 expired offline: a key whose TTL ran out while the server was down
//     is absent after restart (TestEX1ExpiredWhileDown).
//   - EX2 aged offline: a key that survived the downtime comes back with its
//     TTL reduced by the offline interval, from a snapshot
//     (TestEX2TTLAgedWhileDownFromSnapshot) and from the log alone, which
//     proves EXPIRE is logged as an absolute PEXPIREAT
//     (TestEX2TTLAgedWhileDownFromLog).
//   - EX3 passive: an expired key read by GET is reported absent and counted
//     in expired_keys (TestEX3PassiveExpiry).
//   - EX4 active: keys nobody reads again are reclaimed by the sweep alone
//     (TestEX4ActiveExpiryReclaimsUntouchedKeys here against the binary, and
//     TestActiveSweepReclaimsUntouchedKeysAndPingStaysFast in
//     internal/command from T3.02).

// EX1: SET, EXPIRE 2, BGSAVE, kill, 3 s offline, restart: the key is gone
// from GET, EXISTS and KEYS, and recovery counted it as expired.
func TestEX1ExpiredWhileDown(t *testing.T) {
	testutil.SkipIfShort(t, "keeps the server down for 3 s")
	p := testutil.Start(t)
	c := p.Client(t)
	c.MustOK(t, "SET", "s", "v")
	if n := c.MustInt(t, "EXPIRE", "s", "2"); n != 1 {
		t.Fatalf("EXPIRE = %d, want 1", n)
	}
	bgsave(t, c, 10*time.Second)
	p.Kill()
	time.Sleep(3 * time.Second)
	p.Restart(t)

	c = p.Client(t)
	c.MustNil(t, "GET", "s")
	if n := c.MustInt(t, "EXISTS", "s"); n != 0 {
		t.Fatalf("EXISTS s = %d after it expired offline, want 0", n)
	}
	for _, k := range c.MustStrings(t, "KEYS", "*") {
		if k == "s" {
			t.Fatal("KEYS * still lists s after it expired offline")
		}
	}
	if n := infoInt(t, c, "stats", "expired_keys"); n < 1 {
		t.Fatalf("expired_keys = %d after recovery dropped an expired key, want at least 1", n)
	}
}

// EX2 from a snapshot: EXPIRE 100, BGSAVE, kill, 5 s offline, restart: TTL
// must have aged to between 93 and 96, not reset to 100.
func TestEX2TTLAgedWhileDownFromSnapshot(t *testing.T) {
	testutil.SkipIfShort(t, "keeps the server down for 5 s")
	p := testutil.Start(t)
	c := p.Client(t)
	c.MustOK(t, "SET", "s", "v")
	c.MustInt(t, "EXPIRE", "s", "100")
	bgsave(t, c, 10*time.Second)
	assertAgedAfterDowntime(t, p)
}

// EX2 from the log alone: no BGSAVE, so the TTL can only come back right if
// EXPIRE was logged as PEXPIREAT with an absolute deadline.
func TestEX2TTLAgedWhileDownFromLog(t *testing.T) {
	testutil.SkipIfShort(t, "keeps the server down for 5 s")
	p := testutil.Start(t, "--appendfsync", "always")
	c := p.Client(t)
	c.MustOK(t, "SET", "s", "v")
	c.MustInt(t, "EXPIRE", "s", "100")
	assertAgedAfterDowntime(t, p)
}

func assertAgedAfterDowntime(t *testing.T, p *testutil.ServerProcess) {
	t.Helper()
	p.Kill()
	time.Sleep(5 * time.Second)
	p.Restart(t)
	ttl := p.Client(t).MustInt(t, "TTL", "s")
	if ttl < 93 || ttl > 96 {
		t.Fatalf("TTL s = %d after 5 s offline, want between 93 and 96 (a relative TTL would read 100)", ttl)
	}
}

// EX3: an expired key is reported absent on its next read and is counted in
// expired_keys exactly once. During the 2 s wait the active sweep may reclaim
// it before GET does; expired_keys counts either path, as in Redis, so the
// count is exactly one more whichever tier got there first, and GET must be
// nil either way.
func TestEX3PassiveExpiry(t *testing.T) {
	p := testutil.Start(t)
	c := p.Client(t)
	before := infoInt(t, c, "stats", "expired_keys")
	c.MustOK(t, "SET", "s", "v")
	c.MustInt(t, "EXPIRE", "s", "1")
	time.Sleep(2 * time.Second)
	c.MustNil(t, "GET", "s")
	if after := infoInt(t, c, "stats", "expired_keys"); after != before+1 {
		t.Fatalf("expired_keys went from %d to %d, want exactly one more", before, after)
	}
}

// EX4: 10000 keys with a 1 s TTL that are never read again are all reclaimed
// by the active sweep within 5 s, as INFO shows.
func TestEX4ActiveExpiryReclaimsUntouchedKeys(t *testing.T) {
	testutil.SkipIfShort(t, "waits for 10000 keys to expire")
	p := testutil.Start(t)
	c := p.Client(t)
	const n = 10000
	var cmds [][]string
	for i := 0; i < n; i++ {
		k := fmt.Sprintf("e%d", i)
		cmds = append(cmds, []string{"SET", k, "v"}, []string{"EXPIRE", k, "1"})
	}
	if _, err := c.Pipeline(cmds); err != nil {
		t.Fatal(err)
	}
	if db0 := testutil.MustInfo(t, c, "keyspace")["db0"]; db0 != fmt.Sprintf("keys=%d,expires=%d", n, n) {
		t.Fatalf("INFO db0 = %q right after loading", db0)
	}
	testutil.WaitFor(t, 5*time.Second, "db0 keys=0", func() bool {
		db0, ok := testutil.MustInfo(t, c, "keyspace")["db0"]
		return !ok || strings.HasPrefix(db0, "keys=0,")
	})
}
