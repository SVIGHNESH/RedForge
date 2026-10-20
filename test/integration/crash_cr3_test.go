package integration

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/testutil"
)

// bgsaveDelay is how long --debug-bgsave-delay-ms holds the snapshot writer,
// so a SIGKILL lands mid-snapshot deterministically.
const bgsaveDelay = "3000"

// CR3 (PROPOSAL 6.4, MASTER-PLAN 4.6): a crash in the middle of BGSAVE leaves
// the previous dump.snap byte-for-byte untouched plus a dump.snap.tmp-<pid>
// orphan; restart deletes the orphan and loses no data, because the log is
// never cut back on the strength of a snapshot that was not renamed.
func TestCR3KillDuringBgsaveKeepsPreviousSnapshot(t *testing.T) {
	testutil.SkipIfShort(t, "waits for a 3 s delayed BGSAVE")
	p := testutil.Start(t, "--debug-bgsave-delay-ms", bgsaveDelay, "--appendfsync", "always")
	c := p.Client(t)
	snap := filepath.Join(p.DataDir(), "dump.snap")

	a := writeMixed(t, c, "a", 100)
	bgsave(t, c, 8*time.Second)
	first, err := os.ReadFile(snap)
	if err != nil {
		t.Fatalf("first BGSAVE left no dump.snap: %v", err)
	}

	b := writeMixed(t, c, "b", 100)
	startBgsave(t, c)
	time.Sleep(500 * time.Millisecond)
	p.Kill()

	after, err := os.ReadFile(snap)
	if err != nil {
		t.Fatalf("dump.snap missing after a kill during BGSAVE: %v", err)
	}
	if !bytes.Equal(first, after) {
		t.Fatalf("dump.snap changed during an interrupted BGSAVE (%d bytes before, %d after)", len(first), len(after))
	}
	if orphans := tmpSnapshots(t, p); len(orphans) == 0 {
		t.Fatal("no dump.snap.tmp-* file after killing the server mid-BGSAVE")
	}

	p.Restart(t)
	if orphans := tmpSnapshots(t, p); len(orphans) != 0 {
		t.Fatalf("startup left snapshot orphans behind: %v", orphans)
	}
	c = p.Client(t)
	verifyMixed(t, c, a)
	verifyMixed(t, c, b)
}

// CR3 variant: killed during the very first BGSAVE there is no previous
// snapshot, so none may appear, and recovery comes from the log alone.
func TestCR3KillDuringFirstBgsaveRecoversFromLog(t *testing.T) {
	p := testutil.Start(t, "--debug-bgsave-delay-ms", bgsaveDelay, "--appendfsync", "always")
	c := p.Client(t)
	a := writeMixed(t, c, "a", 100)
	startBgsave(t, c)
	time.Sleep(500 * time.Millisecond)
	p.Kill()

	if _, err := os.Stat(filepath.Join(p.DataDir(), "dump.snap")); !os.IsNotExist(err) {
		t.Fatalf("dump.snap exists after the only BGSAVE was killed (stat err %v)", err)
	}
	p.Restart(t)
	if orphans := tmpSnapshots(t, p); len(orphans) != 0 {
		t.Fatalf("startup left snapshot orphans behind: %v", orphans)
	}
	verifyMixed(t, p.Client(t), a)
}

func tmpSnapshots(t *testing.T, p *testutil.ServerProcess) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(p.DataDir(), "dump.snap.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}
