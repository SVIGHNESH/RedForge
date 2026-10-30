package aof

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func fixedNow() func() int64 {
	var ms atomic.Int64
	ms.Store(1700000000000)
	return ms.Load
}

// readAll reads every record from the log in dir and returns them.
func readAll(t *testing.T, dir string) []struct {
	seq  uint64
	args [][]byte
} {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := ReadHeader(f); err != nil {
		t.Fatal(err)
	}
	rd := NewReader(f, int64(HeaderSize))
	var out []struct {
		seq  uint64
		args [][]byte
	}
	for {
		seq, args, err := rd.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("Next after %d records: %v", len(out), err)
		}
		out = append(out, struct {
			seq  uint64
			args [][]byte
		}{seq, args})
	}
}

// Under every policy, 100 appended records come back in order with dense
// sequence numbers after Close.
func TestHundredRecordsEachPolicy(t *testing.T) {
	for _, p := range []Policy{Always, EverySec, No} {
		t.Run(p.String(), func(t *testing.T) {
			dir := t.TempDir()
			w, err := Open(dir, p, fixedNow())
			if err != nil {
				t.Fatal(err)
			}
			for i := 1; i <= 100; i++ {
				if err := w.Append(uint64(i), argv("SET", "k"+strconv.Itoa(i), "v")); err != nil {
					t.Fatal(err)
				}
				w.Tick()
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			recs := readAll(t, dir)
			if len(recs) != 100 {
				t.Fatalf("read %d records, want 100", len(recs))
			}
			for i, r := range recs {
				if r.seq != uint64(i+1) || string(r.args[1]) != "k"+strconv.Itoa(i+1) {
					t.Fatalf("record %d = seq %d %q", i, r.seq, r.args)
				}
			}
		})
	}
}

// Reopening an existing log appends after the old records instead of
// writing a second header.
func TestReopenAppends(t *testing.T) {
	dir := t.TempDir()
	for round := 0; round < 2; round++ {
		w, err := Open(dir, No, fixedNow())
		if err != nil {
			t.Fatal(err)
		}
		if err := w.Append(uint64(round+1), argv("SET", "a", "1")); err != nil {
			t.Fatal(err)
		}
		w.Close()
	}
	if n := len(readAll(t, dir)); n != 2 {
		t.Fatalf("read %d records after reopen, want 2", n)
	}
}

func TestOpenEmptyDirWritesHeaderOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")
	w, err := Open(dir, EverySec, fixedNow())
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, append([]byte(magic), Version)) {
		t.Fatalf("new log = %q, want only the header", b)
	}
}

// fakeFile counts Sync calls and can be told to fail writes.
type fakeFile struct {
	bytes.Buffer
	syncs    atomic.Int64
	failNext bool
}

func (f *fakeFile) Write(p []byte) (int, error) {
	if f.failNext {
		f.failNext = false
		return 0, errors.New("disk full")
	}
	return f.Buffer.Write(p)
}
func (f *fakeFile) Sync() error  { f.syncs.Add(1); return nil }
func (f *fakeFile) Close() error { return nil }

// always syncs once per append, on the calling (loop) thread.
func TestAlwaysSyncsEveryAppend(t *testing.T) {
	ff := &fakeFile{}
	w := newWriter(ff, Always, fixedNow())
	for i := 1; i <= 5; i++ {
		w.Append(uint64(i), argv("SET", "a", "1"))
	}
	if got := ff.syncs.Load(); got != 5 {
		t.Fatalf("syncs = %d, want 5", got)
	}
}

// everysec syncs only when the tick asks, at most once per second of the
// writer's clock, and never from Append itself.
func TestEverySecSyncsFromTheGoroutineOncePerSecond(t *testing.T) {
	ff := &fakeFile{}
	var now atomic.Int64
	now.Store(1000)
	w := newWriter(ff, EverySec, now.Load)
	w.Append(1, argv("SET", "a", "1"))
	if got := ff.syncs.Load(); got != 0 {
		t.Fatalf("Append synced under everysec: %d", got)
	}
	w.Tick() // first tick requests a sync
	waitFor(t, func() bool { return ff.syncs.Load() == 1 })
	for i := 0; i < 9; i++ { // 900 ms of ticks: no new request
		now.Add(100)
		w.Tick()
	}
	time.Sleep(20 * time.Millisecond)
	if got := ff.syncs.Load(); got != 1 {
		t.Fatalf("syncs within one second = %d, want 1", got)
	}
	now.Add(100)
	w.Tick()
	waitFor(t, func() bool { return ff.syncs.Load() == 2 })
	// The fake Sync bumps the counter before the goroutine stores
	// lastFsyncMs, so wait for the timestamp too instead of reading it once.
	waitFor(t, func() bool { return w.lastFsyncMs.Load() == now.Load() })
	if got := ff.syncs.Load(); got != 2 {
		t.Fatalf("syncs after the second request = %d, want 2", got)
	}
	w.Close()
}

func TestNoPolicyNeverSyncsUntilClose(t *testing.T) {
	ff := &fakeFile{}
	w := newWriter(ff, No, fixedNow())
	for i := 1; i <= 3; i++ {
		w.Append(uint64(i), argv("SET", "a", "1"))
		w.Tick()
	}
	if got := ff.syncs.Load(); got != 0 {
		t.Fatalf("syncs = %d, want 0", got)
	}
}

// A failed write is reported in INFO and does not stop later writes.
func TestWriteErrorIsReportedAndRecovers(t *testing.T) {
	ff := &fakeFile{failNext: true}
	w := newWriter(ff, No, fixedNow())
	if err := w.Append(1, argv("SET", "a", "1")); err == nil {
		t.Fatal("Append succeeded on a failing write")
	}
	if got := infoValue(w, "aof_last_write_status"); got != "err" {
		t.Fatalf("aof_last_write_status = %q, want err", got)
	}
	if err := w.Append(2, argv("SET", "a", "2")); err != nil {
		t.Fatal(err)
	}
	if got := infoValue(w, "aof_last_write_status"); got != "ok" {
		t.Fatalf("aof_last_write_status = %q after recovery, want ok", got)
	}
}

func TestParsePolicy(t *testing.T) {
	for _, s := range []string{"always", "everysec", "no"} {
		p, err := ParsePolicy(s)
		if err != nil || p.String() != s {
			t.Errorf("ParsePolicy(%q) = %v, %v", s, p, err)
		}
	}
	if _, err := ParsePolicy("sometimes"); err == nil {
		t.Error("ParsePolicy accepted an unknown policy")
	}
}

func infoValue(w *Writer, key string) string {
	for _, f := range w.Info() {
		if f.Key == key {
			return f.Value
		}
	}
	return ""
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 2 s")
		}
		time.Sleep(time.Millisecond)
	}
}
