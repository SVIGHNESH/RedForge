package integration

import (
	"testing"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/testutil"
)

// The fixture's own control: written and verified on one live server with
// no crash in between, so a CR failure points at recovery, not at the fixture.
func TestMixedFixtureRoundTrip(t *testing.T) {
	c := testutil.Start(t).Client(t)
	verifyMixed(t, c, writeMixed(t, c, "k", 200))
}

// CR1, log only (PROPOSAL 6.4, MASTER-PLAN MS7): with appendfsync always
// every acknowledged write is on disk before its reply, so a SIGKILL straight
// after the last reply loses nothing. Restart must rebuild every key with
// its type, contents and aged TTL from the log alone.
func TestCR1LogOnlyAlways(t *testing.T) {
	p := testutil.Start(t, "--appendfsync", "always")
	model := writeMixed(t, p.Client(t), "k", 200)
	p.Kill()
	p.Restart(t)
	verifyMixed(t, p.Client(t), model)
}

// CR1 with the default everysec policy. Without the pause, losing up to one
// second of acknowledged writes is permitted by the durability window in
// PROPOSAL 6.4; after 1.5 s the fsync goroutine has run, so nothing may be
// missing. (A SIGKILL does not drop the page cache anyway, so in practice
// even the unpaused case keeps everything; the test asserts only what the
// guarantee promises.)
func TestCR1LogOnlyEverysec(t *testing.T) {
	p := testutil.Start(t, "--appendfsync", "everysec")
	model := writeMixed(t, p.Client(t), "k", 200)
	time.Sleep(1500 * time.Millisecond)
	p.Kill()
	p.Restart(t)
	verifyMixed(t, p.Client(t), model)
}

// CR2, snapshot plus log tail: half the data is in dump.snap, the rest only
// in the log after the snapshot's boundary. Both halves must survive a
// SIGKILL, and INFO must show the snapshot's sequence below the log's.
func TestCR2SnapshotPlusTail(t *testing.T) {
	p := testutil.Start(t)
	c := p.Client(t)
	a := writeMixed(t, c, "a", 200)
	bgsave(t, c, 10*time.Second)
	b := writeMixed(t, c, "b", 200)
	time.Sleep(1500 * time.Millisecond) // past the everysec window
	p.Kill()
	p.Restart(t)

	c = p.Client(t)
	verifyMixed(t, c, a)
	verifyMixed(t, c, b)
	snapSeq := infoInt(t, c, "persistence", "last_bgsave_seq")
	logSeq := infoInt(t, c, "persistence", "aof_current_seq")
	if snapSeq <= 0 || snapSeq >= logSeq {
		t.Fatalf("last_bgsave_seq = %d, aof_current_seq = %d; want 0 < snapshot seq < log seq", snapSeq, logSeq)
	}
}
