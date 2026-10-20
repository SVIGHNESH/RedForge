package snapshot

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/command"
	"github.com/SVIGHNESH/RedForge/internal/resp"
	"github.com/SVIGHNESH/RedForge/internal/server"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

// node is an in-process server with a store, a saver and a client, wired
// the way main wires them (no log: these tests are about the snapshot).
type node struct {
	dir   string
	st    store.Store
	saver *Saver
	conn  net.Conn
	buf   []byte
}

func startNode(t *testing.T, delay time.Duration) *node {
	t.Helper()
	dir := t.TempDir()
	clock := store.RealClock{}
	srv := server.New()
	st := store.New(clock)
	command.Install(srv, st, clock)
	sv, err := NewSaver(srv, st, dir, delay, clock.NowMs)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetBackgroundSaver(sv)
	srv.RegisterInfo("Persistence", sv.Info)
	if err := srv.Listen("127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	port, _ := srv.Port()
	done := make(chan error, 1)
	go func() { done <- srv.Serve() }()
	conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		conn.Close()
		srv.Stop()
		<-done
		srv.Close()
		// Wait out a delayed writer so TempDir cleanup does not race it.
		deadline := time.Now().Add(5 * time.Second)
		for sv.InProgress() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		sv.Close()
	})
	return &node{dir: dir, st: st, saver: sv, conn: conn}
}

// do sends one command and returns the parsed reply.
func (n *node) do(t *testing.T, args ...string) resp.Value {
	t.Helper()
	argv := make([][]byte, len(args))
	for i, a := range args {
		argv[i] = []byte(a)
	}
	if _, err := n.conn.Write(resp.ArrayOfBulks(argv).AppendTo(nil)); err != nil {
		t.Fatal(err)
	}
	n.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	chunk := make([]byte, 64*1024)
	for {
		if len(n.buf) > 0 {
			v, used, err := resp.Parse(n.buf)
			if err == nil {
				n.buf = n.buf[used:]
				return v
			}
		}
		m, err := n.conn.Read(chunk)
		if err != nil {
			t.Fatalf("read reply to %v: %v", args, err)
		}
		n.buf = append(n.buf, chunk[:m]...)
	}
}

func (n *node) info(t *testing.T, field string) string {
	t.Helper()
	v := n.do(t, "INFO", "persistence")
	for _, line := range strings.Split(string(v.Str), "\r\n") {
		if k, val, ok := strings.Cut(line, ":"); ok && k == field {
			return val
		}
	}
	t.Fatalf("INFO has no %s", field)
	return ""
}

func (n *node) waitSaved(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for n.info(t, "bgsave_in_progress") != "0" {
		if time.Now().After(deadline) {
			t.Fatal("BGSAVE did not finish within 10 s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (n *node) load(t *testing.T) (uint64, map[string]string) {
	t.Helper()
	f, err := os.Open(filepath.Join(n.dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	maxSeq, entries, err := Read(f)
	if err != nil {
		t.Fatalf("dump.snap does not load: %v", err)
	}
	out := map[string]string{}
	for _, ke := range entries {
		out[ke.Key] = render(ke.Key, ke.Entry)
	}
	return maxSeq, out
}

// BGSAVE writes a verifiable dump.snap whose max_seq is the sequence counter
// at the moment of the call, and reports success in INFO.
func TestBGSAVEWritesTheKeyspaceAtTheBoundary(t *testing.T) {
	n := startNode(t, 0)
	n.do(t, "SET", "a", "1")
	n.do(t, "RPUSH", "l", "x", "y")
	n.do(t, "HSET", "h", "f", "v")
	n.do(t, "SADD", "s", "m")
	n.do(t, "SET", "t", "v")
	n.do(t, "EXPIRE", "t", "100")
	seq := n.info(t, "aof_current_seq")

	if v := n.do(t, "BGSAVE"); v.Kind != resp.KindSimpleString || string(v.Str) != "Background saving started" {
		t.Fatalf("BGSAVE = %+v", v)
	}
	n.waitSaved(t)

	maxSeq, got := n.load(t)
	if strconv.FormatUint(maxSeq, 10) != seq {
		t.Fatalf("max_seq = %d, want aof_current_seq %s", maxSeq, seq)
	}
	if len(got) != 5 {
		t.Fatalf("snapshot has %d keys, want 5: %v", len(got), got)
	}
	if !strings.Contains(got["t"], "exp=") || strings.Contains(got["t"], "exp=0 ") {
		t.Fatalf("expiring key lost its absolute deadline: %s", got["t"])
	}
	if s := n.info(t, "last_bgsave_status"); s != "ok" {
		t.Fatalf("last_bgsave_status = %s", s)
	}
	if s := n.info(t, "last_bgsave_seq"); s != seq {
		t.Fatalf("last_bgsave_seq = %s, want %s", s, seq)
	}
	if matches, _ := filepath.Glob(filepath.Join(n.dir, tmpPrefix+"*")); len(matches) != 0 {
		t.Fatalf("temp files left after a save: %v", matches)
	}
}

// While a delayed save runs: a second BGSAVE is refused, the loop keeps
// answering PING quickly, and writes made during the save are in the live
// store but not in the file, which holds the keyspace as of the boundary.
func TestDuringASaveTheLoopServesAndTheFileIsPointInTime(t *testing.T) {
	n := startNode(t, 700*time.Millisecond)
	n.do(t, "RPUSH", "l", "a", "b")
	n.do(t, "HSET", "h", "f", "1")
	n.do(t, "SET", "doomed", "v")
	n.do(t, "BGSAVE")

	if v := n.do(t, "BGSAVE"); v.Kind != resp.KindError || string(v.Str) != "ERR Background save already in progress" {
		t.Fatalf("second BGSAVE = %+v", v)
	}
	if n.info(t, "bgsave_in_progress") != "1" {
		t.Fatal("bgsave_in_progress is not 1 during a delayed save")
	}

	var worst time.Duration
	for i := 0; i < 50; i++ {
		start := time.Now()
		n.do(t, "PING")
		if d := time.Since(start); d > worst {
			worst = d
		}
	}
	n.do(t, "RPUSH", "l", "c")
	n.do(t, "HSET", "h", "f", "2")
	n.do(t, "DEL", "doomed")
	n.do(t, "SET", "new", "v")
	n.waitSaved(t)
	t.Logf("slowest PING during the save: %v", worst)
	if worst > 50*time.Millisecond {
		t.Fatalf("PING took %v during BGSAVE; the loop is blocked", worst)
	}

	_, got := n.load(t)
	if want := render("l", &store.Entry{Type: store.TList, Val: listOf("a", "b")}); got["l"] != want {
		t.Fatalf("file has %s, want the list as it was at BGSAVE: %s", got["l"], want)
	}
	if !strings.Contains(got["h"], "f=1") {
		t.Fatalf("file has %s, want the old hash value", got["h"])
	}
	if _, ok := got["doomed"]; !ok {
		t.Fatal("key deleted during the save is missing from the file")
	}
	if _, ok := got["new"]; ok {
		t.Fatal("key added during the save is in the file")
	}
	if v := n.do(t, "LRANGE", "l", "0", "-1"); len(v.Array) != 3 {
		t.Fatalf("live list lost the push made during the save: %+v", v)
	}
}

// A failed save reports err in INFO, removes its temp file, and leaves the
// previous dump.snap in place; the next save works.
func TestFailedSaveKeepsThePreviousSnapshot(t *testing.T) {
	n := startNode(t, 0)
	n.do(t, "SET", "a", "1")
	n.do(t, "BGSAVE")
	n.waitSaved(t)
	before, _ := os.ReadFile(filepath.Join(n.dir, FileName))

	// Make the temp file impossible to create.
	if err := os.Chmod(n.dir, 0o500); err != nil {
		t.Fatal(err)
	}
	n.do(t, "SET", "b", "2")
	n.do(t, "BGSAVE")
	n.waitSaved(t)
	os.Chmod(n.dir, 0o755)
	if os.Geteuid() == 0 {
		t.Skip("running as root: permissions do not stop the write")
	}
	if s := n.info(t, "last_bgsave_status"); s != "err" {
		t.Fatalf("last_bgsave_status = %s after a failed save", s)
	}
	after, _ := os.ReadFile(filepath.Join(n.dir, FileName))
	if string(before) != string(after) {
		t.Fatal("a failed save changed dump.snap")
	}

	n.do(t, "BGSAVE")
	n.waitSaved(t)
	if s := n.info(t, "last_bgsave_status"); s != "ok" {
		t.Fatalf("last_bgsave_status = %s after recovery", s)
	}
	if _, got := n.load(t); len(got) != 2 {
		t.Fatalf("snapshot after recovery has %d keys, want 2", len(got))
	}
}

func TestCleanOrphans(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{tmpPrefix + "123", tmpPrefix + "456", FileName, "appendonly.aof"} {
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644)
	}
	n, err := CleanOrphans(dir)
	if err != nil || n != 2 {
		t.Fatalf("CleanOrphans = %d, %v; want 2", n, err)
	}
	left, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(left) != 2 {
		t.Fatalf("left %v, want dump.snap and appendonly.aof", left)
	}
}

// WriteFile never exposes a partial dump.snap: until the rename the old file
// is intact, even if the writer is stopped in the delay window.
func TestWriteFileIsAtomic(t *testing.T) {
	dir := t.TempDir()
	old := sample()
	if err := WriteFile(dir, 1, old, 0); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.ReadFile(filepath.Join(dir, FileName))

	next := store.New(nil)
	next.Put("only", &store.Entry{Type: store.TString, Val: &store.StringVal{B: []byte("new")}})
	errc := make(chan error, 1)
	go func() { errc <- WriteFile(dir, 2, next, 300*time.Millisecond) }()
	time.Sleep(100 * time.Millisecond)
	mid, _ := os.ReadFile(filepath.Join(dir, FileName))
	if string(mid) != string(prev) {
		t.Fatal("dump.snap changed before the rename")
	}
	if tmps, _ := filepath.Glob(filepath.Join(dir, tmpPrefix+"*")); len(tmps) != 1 {
		t.Fatalf("temp files during the delay: %v", tmps)
	}
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(filepath.Join(dir, FileName))
	defer f.Close()
	maxSeq, entries, err := Read(f)
	if err != nil || maxSeq != 2 || len(entries) != 1 {
		t.Fatalf("after rename: %d %d %v", maxSeq, len(entries), err)
	}
}

func TestBGSAVEWithoutASaverIsAnError(t *testing.T) {
	srv := server.New()
	ctx := &command.Ctx{Client: &server.Client{}, Store: store.New(nil), Srv: srv}
	v := command.Dispatch(ctx, [][]byte{[]byte("BGSAVE")})
	if !resp.IsError(v) {
		t.Fatalf("BGSAVE with no saver = %q", v.AppendTo(nil))
	}
}

func listOf(items ...string) *store.ListVal {
	l := &store.ListVal{}
	for _, x := range items {
		l.PushBack([]byte(x))
	}
	return l
}
