package integration

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/resp"
	"github.com/SVIGHNESH/RedForge/internal/testutil"
)

// The crash-recovery and expiry tests are written from the guarantees in
// PROPOSAL 6.3 and 6.4 and the MASTER-PLAN 4.7 to 4.10 contracts, never from
// the persistence code. They rely only on:
//
//   - flags: --appendfsync always|everysec|no, --debug-bgsave-delay-ms N
//   - files: the default --dir ./data, holding appendonly.aof, dump.snap and
//     dump.snap.tmp-<pid> while a BGSAVE is writing
//   - INFO: aof_current_seq, aof_truncated_records,
//     aof_last_truncation_offset, bgsave_in_progress, last_bgsave_status,
//     last_bgsave_seq, expired_keys, db0:keys=N,expires=M
//
// If one of these tests fails once persistence lands, the persistence code is
// wrong until proven otherwise.

// mixedTTL is the TTL in seconds every fifth key gets in writeMixed.
const mixedTTL = 1000

// mixedEntry is what a key written by writeMixed must hold after recovery.
type mixedEntry struct {
	kind    string   // "string", "list", "hash" or "set"
	value   []string // string: [v]; list: items head to tail; hash: sorted field,value pairs; set: sorted members
	expires bool
}

// mixedModel maps every key writeMixed wrote to its expected contents.
type mixedModel map[string]mixedEntry

// writeMixed writes n strings, n lists of 3 items, n hashes of 3 fields and n
// sets of 3 members under prefix, gives every fifth key EXPIRE 1000, and
// returns the model to verify against. Writes are pipelined; every reply is
// checked, so a write the server rejected fails here rather than later.
func writeMixed(t *testing.T, c *testutil.Client, prefix string, n int) mixedModel {
	t.Helper()
	model := mixedModel{}
	var cmds [][]string
	k := 0
	add := func(key string, e mixedEntry, write []string) {
		cmds = append(cmds, write)
		if k%5 == 0 {
			e.expires = true
			cmds = append(cmds, []string{"EXPIRE", key, strconv.Itoa(mixedTTL)})
		}
		model[key] = e
		k++
	}
	for i := 0; i < n; i++ {
		s := strconv.Itoa(i)
		add(prefix+":s:"+s, mixedEntry{kind: "string", value: []string{"v" + s}},
			[]string{"SET", prefix + ":s:" + s, "v" + s})
		add(prefix+":l:"+s, mixedEntry{kind: "list", value: []string{"a" + s, "b" + s, "c" + s}},
			[]string{"RPUSH", prefix + ":l:" + s, "a" + s, "b" + s, "c" + s})
		add(prefix+":h:"+s, mixedEntry{kind: "hash", value: []string{"f1", "x" + s, "f2", "y" + s, "f3", "z" + s}},
			[]string{"HSET", prefix + ":h:" + s, "f1", "x" + s, "f2", "y" + s, "f3", "z" + s})
		add(prefix+":z:"+s, mixedEntry{kind: "set", value: []string{"m1" + s, "m2" + s, "m3" + s}},
			[]string{"SADD", prefix + ":z:" + s, "m1" + s, "m2" + s, "m3" + s})
	}
	replies, err := c.Pipeline(cmds)
	if err != nil {
		t.Fatalf("writeMixed %s: %v", prefix, err)
	}
	for i, v := range replies {
		if v.Kind == resp.KindError {
			t.Fatalf("writeMixed %s: %v replied %s", prefix, cmds[i], testutil.Show(v))
		}
	}
	return model
}

// verifyMixed checks that every key in model exists with the right type and
// contents, and that the expiring keys kept a TTL that has only aged (between
// 900 and 1000 s) while the others have none.
func verifyMixed(t *testing.T, c *testutil.Client, model mixedModel) {
	t.Helper()
	keys := make([]string, 0, len(model))
	for k := range model {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var cmds [][]string
	for _, k := range keys {
		switch model[k].kind {
		case "string":
			cmds = append(cmds, []string{"GET", k})
		case "list":
			cmds = append(cmds, []string{"LRANGE", k, "0", "-1"})
		case "hash":
			cmds = append(cmds, []string{"HGETALL", k})
		case "set":
			cmds = append(cmds, []string{"SMEMBERS", k})
		}
		cmds = append(cmds, []string{"TTL", k})
	}
	replies, err := c.Pipeline(cmds)
	if err != nil {
		t.Fatalf("verifyMixed: %v", err)
	}
	for i, k := range keys {
		e := model[k]
		got, ttl := replies[2*i], replies[2*i+1]
		if err := matchEntry(e, got); err != nil {
			t.Fatalf("key %s (%s): %v", k, e.kind, err)
		}
		if ttl.Kind != resp.KindInteger {
			t.Fatalf("TTL %s replied %s", k, testutil.Show(ttl))
		}
		switch {
		case e.expires && (ttl.Int < mixedTTL-100 || ttl.Int > mixedTTL):
			t.Fatalf("TTL %s = %d, want between %d and %d (aged, never reset)", k, ttl.Int, mixedTTL-100, mixedTTL)
		case !e.expires && ttl.Int != -1:
			t.Fatalf("TTL %s = %d, want -1 (no expiry)", k, ttl.Int)
		}
	}
}

func matchEntry(e mixedEntry, got resp.Value) error {
	if got.Kind == resp.KindError {
		return fmt.Errorf("replied %s", testutil.Show(got))
	}
	if e.kind == "string" {
		if got.Kind != resp.KindBulk || got.Null || string(got.Str) != e.value[0] {
			return fmt.Errorf("= %s, want %q", testutil.Show(got), e.value[0])
		}
		return nil
	}
	if got.Kind != resp.KindArray || got.Null {
		return fmt.Errorf("= %s, want an array", testutil.Show(got))
	}
	items := make([]string, len(got.Array))
	for i, a := range got.Array {
		items[i] = string(a.Str)
	}
	switch e.kind {
	case "set":
		sort.Strings(items)
	case "hash":
		items = sortPairs(items)
	}
	if strings.Join(items, "\x00") != strings.Join(e.value, "\x00") {
		return fmt.Errorf("= %q, want %q", items, e.value)
	}
	return nil
}

// sortPairs sorts a flat field,value list by field.
func sortPairs(flat []string) []string {
	type pair struct{ f, v string }
	ps := make([]pair, 0, len(flat)/2)
	for i := 0; i+1 < len(flat); i += 2 {
		ps = append(ps, pair{flat[i], flat[i+1]})
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].f < ps[j].f })
	out := make([]string, 0, len(flat))
	for _, p := range ps {
		out = append(out, p.f, p.v)
	}
	return out
}

// bgsave starts a BGSAVE and waits until INFO reports it finished and
// succeeded.
func bgsave(t *testing.T, c *testutil.Client, timeout time.Duration) {
	t.Helper()
	startBgsave(t, c)
	waitBgsaveDone(t, c, timeout)
}

// startBgsave sends BGSAVE and checks Redis's reply.
func startBgsave(t *testing.T, c *testutil.Client) {
	t.Helper()
	v := c.MustDo(t, "BGSAVE")
	if v.Kind != resp.KindSimpleString || string(v.Str) != "Background saving started" {
		t.Fatalf("BGSAVE replied %s, want +Background saving started", testutil.Show(v))
	}
}

func waitBgsaveDone(t *testing.T, c *testutil.Client, timeout time.Duration) {
	t.Helper()
	testutil.WaitFor(t, timeout, "bgsave_in_progress:0", func() bool {
		return testutil.MustInfo(t, c, "persistence")["bgsave_in_progress"] == "0"
	})
	if s := testutil.MustInfo(t, c, "persistence")["last_bgsave_status"]; s != "ok" {
		t.Fatalf("last_bgsave_status = %q after BGSAVE, want ok", s)
	}
}

// infoInt reads one numeric INFO field.
func infoInt(t *testing.T, c *testutil.Client, section, field string) int64 {
	t.Helper()
	raw, ok := testutil.MustInfo(t, c, section)[field]
	if !ok {
		t.Fatalf("INFO %s has no %s field", section, field)
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		t.Fatalf("INFO %s = %q, not a number", field, raw)
	}
	return n
}
