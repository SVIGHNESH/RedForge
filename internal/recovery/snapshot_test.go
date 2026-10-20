package recovery

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/SVIGHNESH/RedForge/internal/aof"
	"github.com/SVIGHNESH/RedForge/internal/snapshot"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

// save writes dump.snap exactly as BGSAVE would: the store's point-in-time
// view at the current sequence boundary.
func (in *instance) save(t *testing.T) uint64 {
	t.Helper()
	boundary := in.srv.CurrentSeq()
	view := in.st.Snapshot()
	defer view.Release()
	if err := snapshot.WriteFile(in.dir, boundary, view, 0); err != nil {
		t.Fatal(err)
	}
	return boundary
}

// Write, snapshot, write more, restart: both halves are present, the log
// records already in the snapshot are skipped, and the counter resumes at
// the last record, above the snapshot's boundary (the CR2 shape).
func TestSnapshotPlusLogTail(t *testing.T) {
	dir := t.TempDir()
	a := boot(t, dir, t0)
	a.do(t, "SET", "before", "1")
	a.do(t, "RPUSH", "l", "x")
	a.do(t, "INCR", "n")
	boundary := a.save(t)
	a.do(t, "SET", "after", "2")
	a.do(t, "RPUSH", "l", "y")
	a.do(t, "INCR", "n")
	final := a.srv.CurrentSeq()
	a.stop(t)

	b := boot(t, dir, t0+1000)
	if b.res.Snapshot != SnapshotOK || b.res.SnapshotSeq != boundary || b.res.SnapshotKeys != 3 {
		t.Fatalf("snapshot step: %+v", b.res)
	}
	if b.res.Log.Skipped != int(boundary) || b.res.Log.Applied != int(final-boundary) {
		t.Fatalf("replay skipped %d applied %d, want %d and %d", b.res.Log.Skipped, b.res.Log.Applied, boundary, final-boundary)
	}
	if b.res.LastSeq != final || b.srv.CurrentSeq() != final {
		t.Fatalf("seq resumed at %d, want %d", b.res.LastSeq, final)
	}
	if got := b.do(t, "GET", "before"); got != bulk("1") {
		t.Fatalf("GET before = %q", got)
	}
	if got := b.do(t, "GET", "after"); got != bulk("2") {
		t.Fatalf("GET after = %q", got)
	}
	if got := b.do(t, "LRANGE", "l", "0", "-1"); got != "*2\r\n"+bulk("x")+bulk("y") {
		t.Fatalf("LRANGE = %q: a record applied twice or missed", got)
	}
	if got := b.do(t, "GET", "n"); got != bulk("2") {
		t.Fatalf("INCR counter = %q, want 2 (each INCR applied exactly once)", got)
	}
	b.stop(t)
}

// A corrupt dump.snap is ignored with a warning and the server recovers
// everything from the log alone.
func TestCorruptSnapshotFallsBackToTheLog(t *testing.T) {
	dir := t.TempDir()
	a := boot(t, dir, t0)
	for i := 0; i < 10; i++ {
		a.do(t, "SET", "k"+strconv.Itoa(i), "v")
	}
	a.save(t)
	a.do(t, "SET", "tail", "v")
	a.stop(t)

	path := filepath.Join(dir, snapshot.FileName)
	b, _ := os.ReadFile(path)
	b[len(b)/2] ^= 0xff
	os.WriteFile(path, b, 0o644)

	c := boot(t, dir, t0+1000)
	if c.res.Snapshot != SnapshotErr || c.res.SnapshotSeq != 0 {
		t.Fatalf("corrupt snapshot result %+v", c.res)
	}
	if c.st.Len() != 11 || c.res.LastSeq != 11 {
		t.Fatalf("log-only recovery: %d keys at seq %d, want 11 at 11", c.st.Len(), c.res.LastSeq)
	}
	if v := infoValue(c.res, "snapshot_last_load_status"); v != "err" {
		t.Fatalf("snapshot_last_load_status = %q", v)
	}
	c.stop(t)
}

// Orphaned temp files from a save that crashed are deleted, and the previous
// dump.snap is what loads.
func TestOrphansAreRemovedAndThePreviousSnapshotLoads(t *testing.T) {
	dir := t.TempDir()
	a := boot(t, dir, t0)
	a.do(t, "SET", "a", "1")
	a.save(t)
	a.do(t, "SET", "b", "2")
	a.stop(t)
	orphan := filepath.Join(dir, snapshot.FileName+".tmp-99999")
	os.WriteFile(orphan, []byte("half a snapshot"), 0o644)

	b := boot(t, dir, t0)
	if b.res.OrphansRemoved != 1 {
		t.Fatalf("OrphansRemoved = %d", b.res.OrphansRemoved)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("orphan still present after startup")
	}
	if b.res.Snapshot != SnapshotOK || b.st.Len() != 2 {
		t.Fatalf("res %+v len %d", b.res, b.st.Len())
	}
	b.stop(t)
}

// TTLs restored from a snapshot are aged by the downtime, and a key that
// expired while down is dropped and counted, exactly as from the log.
func TestSnapshotTTLsAreAbsolute(t *testing.T) {
	dir := t.TempDir()
	a := boot(t, dir, t0)
	a.do(t, "SET", "long", "v")
	a.do(t, "EXPIRE", "long", "100")
	a.do(t, "SET", "short", "v")
	a.do(t, "EXPIRE", "short", "2")
	a.save(t)
	a.stop(t)
	// Remove the log so the keys can only come from the snapshot.
	os.Remove(filepath.Join(dir, aof.FileName))

	b := boot(t, dir, t0+5000)
	if got := b.do(t, "TTL", "long"); got != ":95\r\n" {
		t.Fatalf("TTL long after 5 s down = %q, want :95", got)
	}
	if b.res.Expired != 1 || b.st.Exists("short") {
		t.Fatalf("short-lived key: expired %d exists %v", b.res.Expired, b.st.Exists("short"))
	}
	if got := b.st.(store.Counters).ExpiredKeys(); got != 1 {
		t.Fatalf("expired_keys = %d", got)
	}
	// With no log, the counter resumes from the snapshot's max_seq.
	if b.res.LastSeq != 4 {
		t.Fatalf("seq = %d, want the snapshot's max_seq 4", b.res.LastSeq)
	}
	b.stop(t)
}

// An empty directory starts with no snapshot and creates only the log.
func TestEmptyDirHasNoSnapshot(t *testing.T) {
	dir := t.TempDir()
	a := boot(t, dir, t0)
	if a.res.Snapshot != SnapshotNone {
		t.Fatalf("Snapshot = %q", a.res.Snapshot)
	}
	a.stop(t)
	if _, err := os.Stat(filepath.Join(dir, snapshot.FileName)); !os.IsNotExist(err) {
		t.Fatal("startup created a snapshot")
	}
}

func infoValue(r Result, key string) string {
	for _, f := range r.Info() {
		if f.Key == key {
			return f.Value
		}
	}
	return ""
}
