package recovery

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/aof"
	"github.com/SVIGHNESH/RedForge/internal/command"
	"github.com/SVIGHNESH/RedForge/internal/resp"
	"github.com/SVIGHNESH/RedForge/internal/server"
	"github.com/SVIGHNESH/RedForge/internal/store"
	"github.com/SVIGHNESH/RedForge/internal/storetest"
)

const t0 = 1700000000000

// instance is one server lifetime over dir: a store, a server, the log
// writer and a fake clock, wired the way main wires them.
type instance struct {
	dir   string
	clock *storetest.FakeClock
	st    store.Store
	srv   *server.Server
	w     *aof.Writer
	res   Result
}

// boot recovers dir at time nowMs and opens the log for append.
func boot(t *testing.T, dir string, nowMs int64) *instance {
	t.Helper()
	in := &instance{dir: dir, clock: &storetest.FakeClock{}}
	in.clock.Set(nowMs)
	in.st = store.New(in.clock)
	in.srv = server.New()
	res, err := Run(dir, in.st, in.srv, in.clock)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	in.res = res
	w, err := aof.Open(dir, aof.No, in.clock.NowMs)
	if err != nil {
		t.Fatal(err)
	}
	in.w = w
	in.srv.Appenders = []server.Appender{w}
	return in
}

func (in *instance) do(t *testing.T, args ...string) string {
	t.Helper()
	argv := make([][]byte, len(args))
	for i, a := range args {
		argv[i] = []byte(a)
	}
	ctx := &command.Ctx{Client: &server.Client{}, Store: in.st, Srv: in.srv, Now: in.clock.NowMs()}
	return string(command.Dispatch(ctx, argv).AppendTo(nil))
}

func (in *instance) stop(t *testing.T) {
	t.Helper()
	if err := in.w.Close(); err != nil {
		t.Fatal(err)
	}
}

func bulk(s string) string { return string(resp.BulkString(s).AppendTo(nil)) }

// A clean stop and restart recovers every key and type, TTLs aged by the
// downtime, and the sequence counter resumes where it stopped.
func TestRestartRecoversKeysAndAgesTTLs(t *testing.T) {
	dir := t.TempDir()
	a := boot(t, dir, t0)
	a.do(t, "SET", "a", "1")
	a.do(t, "EXPIRE", "a", "100")
	a.do(t, "RPUSH", "l", "x", "y")
	a.do(t, "HSET", "h", "f", "v")
	a.do(t, "SADD", "s", "m")
	a.do(t, "INCR", "n")
	seq := a.srv.CurrentSeq()
	a.stop(t)

	b := boot(t, dir, t0+30_000) // 30 s of downtime
	if got := b.do(t, "GET", "a"); got != bulk("1") {
		t.Fatalf("GET a = %q", got)
	}
	if got := b.do(t, "TTL", "a"); got != ":70\r\n" {
		t.Fatalf("TTL a after 30 s down = %q, want :70", got)
	}
	if got := b.do(t, "LRANGE", "l", "0", "-1"); got != "*2\r\n"+bulk("x")+bulk("y") {
		t.Fatalf("LRANGE = %q", got)
	}
	if got := b.do(t, "HGET", "h", "f"); got != bulk("v") {
		t.Fatalf("HGET = %q", got)
	}
	if got := b.do(t, "SISMEMBER", "s", "m"); got != ":1\r\n" {
		t.Fatalf("SISMEMBER = %q", got)
	}
	if got := b.do(t, "INCR", "n"); got != ":2\r\n" {
		t.Fatalf("INCR after restart = %q, want :2", got)
	}
	if b.res.LastSeq != seq || b.res.Log.Applied != int(seq) {
		t.Fatalf("resumed at seq %d after %d records, want %d", b.res.LastSeq, b.res.Log.Applied, seq)
	}
	// The write after restart got the next seq, so a third boot sees a
	// dense log.
	if b.srv.CurrentSeq() != seq+1 {
		t.Fatalf("seq after one more write = %d, want %d", b.srv.CurrentSeq(), seq+1)
	}
	b.stop(t)
	c := boot(t, dir, t0+31_000)
	if c.res.LastSeq != seq+1 {
		t.Fatalf("third boot resumed at %d, want %d", c.res.LastSeq, seq+1)
	}
	c.stop(t)
}

// A key whose absolute expiry passed while the server was down is gone
// after restart, from every view, and counted in expired_keys.
func TestKeyExpiredWhileDownIsDroppedAndCounted(t *testing.T) {
	dir := t.TempDir()
	a := boot(t, dir, t0)
	a.do(t, "SET", "short", "v")
	a.do(t, "EXPIRE", "short", "2")
	a.do(t, "SET", "long", "v")
	a.do(t, "EXPIRE", "long", "100")
	a.stop(t)

	b := boot(t, dir, t0+3_000)
	if b.res.Expired != 1 {
		t.Fatalf("Expired = %d, want 1", b.res.Expired)
	}
	if got := b.st.(store.Counters).ExpiredKeys(); got != 1 {
		t.Fatalf("expired_keys = %d, want 1", got)
	}
	if got := b.do(t, "EXISTS", "short"); got != ":0\r\n" {
		t.Fatalf("EXISTS short = %q", got)
	}
	if got := b.do(t, "KEYS", "*"); got != "*1\r\n"+bulk("long") {
		t.Fatalf("KEYS * = %q, want only long", got)
	}
	b.stop(t)
}

// Startup on an empty directory creates nothing but the log header.
func TestEmptyDirStartsEmptyWithAHeader(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fresh")
	a := boot(t, dir, t0)
	if a.st.Len() != 0 || a.res.LastSeq != 0 || a.res.Log.TruncatedAt != -1 {
		t.Fatalf("fresh start: len %d res %+v", a.st.Len(), a.res)
	}
	a.stop(t)
	b, err := os.ReadFile(filepath.Join(dir, aof.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "RFAOF\x01" {
		t.Fatalf("log = %q, want just the header", b)
	}
}

// A torn tail is cut during recovery and the server carries on appending
// from the last good record's seq.
func TestTornTailRecoversThePrefixAndContinues(t *testing.T) {
	dir := t.TempDir()
	a := boot(t, dir, t0)
	for i := 0; i < 5; i++ {
		a.do(t, "SET", "k"+strconv.Itoa(i), "v")
	}
	a.stop(t)
	path := filepath.Join(dir, aof.FileName)
	st, _ := os.Stat(path)
	os.Truncate(path, st.Size()-7)

	b := boot(t, dir, t0+1000)
	if b.res.Log.Truncated != 1 || b.res.LastSeq != 4 || b.st.Len() != 4 {
		t.Fatalf("after torn tail: res %+v len %d, want 4 keys at seq 4", b.res, b.st.Len())
	}
	b.do(t, "SET", "k4", "again")
	b.stop(t)

	c := boot(t, dir, t0+2000)
	if c.res.Log.Truncated != 0 || c.res.LastSeq != 5 || c.st.Len() != 5 {
		t.Fatalf("third boot: res %+v len %d, want a clean log with 5 keys", c.res, c.st.Len())
	}
	c.stop(t)
}

func TestRecoversTwentyThousandRecords(t *testing.T) {
	dir := t.TempDir()
	a := boot(t, dir, t0)
	for i := 0; i < 20000; i++ {
		a.do(t, "SET", "k"+strconv.Itoa(i), "v")
	}
	a.stop(t)
	start := time.Now()
	b := boot(t, dir, t0)
	if b.st.Len() != 20000 {
		t.Fatalf("recovered %d keys, want 20000", b.st.Len())
	}
	t.Logf("recovered 20000 records in %s", time.Since(start))
	b.stop(t)
}
