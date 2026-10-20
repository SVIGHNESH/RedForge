package command

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/SVIGHNESH/RedForge/internal/aof"
	"github.com/SVIGHNESH/RedForge/internal/server"
)

// Every propagated write reaches the log through the dispatcher with its
// canonical form and sequence number; reads and failed writes add nothing.
// This is T5.02's wiring, checked without a socket.
func TestDispatchWritesCanonicalFormsToTheLog(t *testing.T) {
	dir := t.TempDir()
	ctx := newCtx()
	w, err := aof.Open(dir, aof.Always, func() int64 { return ctx.Now })
	if err != nil {
		t.Fatal(err)
	}
	ctx.Srv.Appenders = []server.Appender{w}

	reply(t, ctx, "SET", "a", "1")
	reply(t, ctx, "GET", "a")             // read: no record
	reply(t, ctx, "INCR", "missingarity") // write
	reply(t, ctx, "EXPIRE", "a", "100")   // rewritten to PEXPIREAT
	reply(t, ctx, "LPUSH", "a", "x")      // WRONGTYPE: no record
	reply(t, ctx, "DEL", "nope")          // no-op: no record
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(filepath.Join(dir, aof.FileName))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := aof.ReadHeader(f); err != nil {
		t.Fatal(err)
	}
	rd := aof.NewReader(f, int64(aof.HeaderSize))
	want := [][]string{
		{"SET", "a", "1"},
		{"INCR", "missingarity"},
		{"PEXPIREAT", "a", strconv.FormatInt(ctx.Now+100_000, 10)},
	}
	for i, w := range want {
		seq, args, err := rd.Next()
		if err != nil {
			t.Fatalf("record %d: %v", i+1, err)
		}
		if seq != uint64(i+1) {
			t.Fatalf("record %d seq = %d", i+1, seq)
		}
		got := make([]string, len(args))
		for j, a := range args {
			got[j] = string(a)
		}
		if len(got) != len(w) {
			t.Fatalf("record %d = %q, want %q", i+1, got, w)
		}
		for j := range w {
			if !eqFold(j, got[j], w[j]) {
				t.Fatalf("record %d = %q, want %q", i+1, got, w)
			}
		}
	}
	if _, _, err := rd.Next(); err != io.EOF {
		t.Fatalf("extra record after the three writes: err = %v", err)
	}
}

// eqFold compares the command name case-insensitively and arguments exactly.
func eqFold(i int, a, b string) bool {
	if i == 0 {
		return strings.EqualFold(a, b)
	}
	return a == b
}
