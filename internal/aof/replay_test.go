package aof

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeLog writes a log of n "SET k<i> v" records with seqs 1..n and returns
// its path and the offset where each record starts.
func writeLog(t *testing.T, n int) (string, []int64) {
	t.Helper()
	data := append([]byte(magic), Version)
	var starts []int64
	for i := 1; i <= n; i++ {
		starts = append(starts, int64(len(data)))
		data = Encode(data, uint64(i), argv("SET", "k", "v"))
	}
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path, starts
}

func collect(t *testing.T, path string, after uint64) ([]uint64, uint64, ReplayResult) {
	t.Helper()
	var seqs []uint64
	last, res, err := Replay(path, after, func(seq uint64, _ [][]byte) { seqs = append(seqs, seq) })
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	return seqs, last, res
}

func size(t *testing.T, path string) int64 {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return st.Size()
}

func TestReplayCleanLog(t *testing.T) {
	path, _ := writeLog(t, 5)
	before := size(t, path)
	seqs, last, res := collect(t, path, 0)
	if len(seqs) != 5 || last != 5 || res.Applied != 5 {
		t.Fatalf("seqs %v last %d applied %d, want 5 records", seqs, last, res.Applied)
	}
	if res.TruncatedAt != -1 || res.Truncated != 0 {
		t.Fatalf("clean log reported truncation %+v", res)
	}
	if size(t, path) != before {
		t.Fatal("a clean replay changed the file")
	}
}

// Records at or below afterSeq are already in the snapshot: they are not
// applied, but they still count towards the last seq.
func TestReplaySkipsUpToAfterSeq(t *testing.T) {
	path, _ := writeLog(t, 5)
	seqs, last, res := collect(t, path, 2)
	if len(seqs) != 3 || seqs[0] != 3 || last != 5 || res.Skipped != 2 {
		t.Fatalf("seqs %v last %d skipped %d, want [3 4 5], 5, 2", seqs, last, res.Skipped)
	}
}

// A log cut inside its last record is truncated back to that record's start;
// the earlier records are applied and a second replay is clean.
func TestReplayTruncatesATornTail(t *testing.T) {
	path, starts := writeLog(t, 3)
	if err := os.Truncate(path, size(t, path)-7); err != nil {
		t.Fatal(err)
	}
	seqs, last, res := collect(t, path, 0)
	if len(seqs) != 2 || last != 2 {
		t.Fatalf("seqs %v last %d, want two records", seqs, last)
	}
	if res.TruncatedAt != starts[2] || res.Truncated != 1 || !errors.Is(res.Reason, ErrTorn) {
		t.Fatalf("result %+v, want truncation at %d", res, starts[2])
	}
	if got := size(t, path); got != starts[2] {
		t.Fatalf("file is %d bytes, want %d", got, starts[2])
	}
	if _, _, res2 := collect(t, path, 0); res2.Truncated != 0 {
		t.Fatalf("second replay still truncating: %+v", res2)
	}
}

// A CRC failure in the middle ends replay there: every later record, even a
// valid one, is cut from the file.
func TestReplayCorruptMiddleDropsTheRest(t *testing.T) {
	path, starts := writeLog(t, 5)
	b, _ := os.ReadFile(path)
	b[starts[2]+recordHeaderSize+2] ^= 0xff
	os.WriteFile(path, b, 0o644)

	seqs, last, res := collect(t, path, 0)
	if len(seqs) != 2 || last != 2 {
		t.Fatalf("seqs %v last %d, want records 1 and 2 only", seqs, last)
	}
	if !errors.Is(res.Reason, ErrCorrupt) || res.TruncatedAt != starts[2] {
		t.Fatalf("result %+v", res)
	}
	if got := size(t, path); got != starts[2] {
		t.Fatalf("file is %d bytes, want %d (records 3 to 5 gone)", got, starts[2])
	}
}

// After truncation the writer appends right where the good records end.
func TestAppendAfterTruncationIsReadable(t *testing.T) {
	path, _ := writeLog(t, 3)
	os.Truncate(path, size(t, path)-3)
	collect(t, path, 0)
	w, err := Open(filepath.Dir(path), No, fixedNow())
	if err != nil {
		t.Fatal(err)
	}
	w.Append(3, argv("SET", "after", "cut"))
	w.Close()
	seqs, _, res := collect(t, path, 0)
	if len(seqs) != 3 || seqs[2] != 3 || res.Truncated != 0 {
		t.Fatalf("seqs %v res %+v, want 1 2 3 and a clean log", seqs, res)
	}
}

func TestReplayMissingFileIsEmpty(t *testing.T) {
	seqs, last, res := collect(t, filepath.Join(t.TempDir(), FileName), 0)
	if len(seqs) != 0 || last != 0 || res.TruncatedAt != -1 {
		t.Fatalf("missing log: seqs %v last %d res %+v", seqs, last, res)
	}
}

// A crash while the log was being created leaves part of the header; replay
// cuts it to zero so Open writes a fresh one.
func TestReplayTornHeaderIsCutToZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	os.WriteFile(path, []byte("RFA"), 0o644)
	_, _, res := collect(t, path, 0)
	if res.Truncated != 1 || res.TruncatedAt != 0 || size(t, path) != 0 {
		t.Fatalf("res %+v size %d, want cut to 0", res, size(t, path))
	}
}

// A file that is not a RedForge log is refused, not truncated.
func TestReplayForeignFileIsRefusedAndKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	os.WriteFile(path, []byte("REDIS0011 something else"), 0o644)
	_, _, err := Replay(path, 0, func(uint64, [][]byte) {})
	if !errors.Is(err, ErrBadHeader) {
		t.Fatalf("err = %v, want ErrBadHeader", err)
	}
	if size(t, path) != 24 {
		t.Fatal("a foreign file was modified")
	}
}
