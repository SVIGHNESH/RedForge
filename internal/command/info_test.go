package command

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/server"
)

// infoFields is every field MASTER-PLAN Section 4.10 and T3.05 require, by
// section, in output order.
var infoFields = map[string][]string{
	"Server":      {"redis_version", "uptime_in_seconds", "tcp_port", "process_id", "goroutines"},
	"Clients":     {"connected_clients", "blocked_clients"},
	"Memory":      {"used_memory"},
	"Stats":       {"total_connections_received", "total_commands_processed", "expired_keys", "total_sweep_deleted", "keyspace_hits", "keyspace_misses", "keys_last_iter_us", "keys_last_reply_bytes"},
	"Persistence": {"aof_fsync_policy", "aof_current_seq", "aof_last_truncation_offset", "aof_truncated_records", "bgsave_in_progress", "last_bgsave_status", "last_bgsave_seq", "last_bgsave_clone_ms"},
	"Replication": {"role", "connected_replicas"},
	"Keyspace":    {"db0"},
}

var infoOrder = []string{"Server", "Clients", "Memory", "Stats", "Persistence", "Replication", "Keyspace"}

// parsedInfo is INFO output as sections of ordered key:value pairs.
type parsedInfo struct {
	sections []string
	keys     map[string][]string
	values   map[string]string
}

// parseInfo parses the bulk payload of INFO strictly: CRLF line endings,
// "# Name" headers, key:value lines, and single blank lines only between
// sections. It is the parser T1.03's testutil.Info will mirror.
func parseInfo(t *testing.T, payload string) parsedInfo {
	t.Helper()
	p := parsedInfo{keys: map[string][]string{}, values: map[string]string{}}
	if !strings.HasSuffix(payload, "\r\n") {
		t.Fatalf("INFO payload does not end in CRLF: %q", payload)
	}
	lines := strings.Split(strings.TrimSuffix(payload, "\r\n"), "\r\n")
	cur := ""
	for i, line := range lines {
		switch {
		case strings.Contains(line, "\n") || strings.Contains(line, "\r"):
			t.Fatalf("line %d has a bare CR or LF: %q", i, line)
		case strings.HasPrefix(line, "# "):
			if i > 0 && lines[i-1] != "" {
				t.Fatalf("section %q is not preceded by a blank line", line)
			}
			cur = line[2:]
			p.sections = append(p.sections, cur)
		case line == "":
			if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "# ") {
				t.Fatalf("blank line %d is not followed by a section header", i)
			}
		default:
			k, v, ok := strings.Cut(line, ":")
			if !ok || cur == "" {
				t.Fatalf("line %d %q is not key:value inside a section", i, line)
			}
			p.keys[cur] = append(p.keys[cur], k)
			p.values[k] = v
		}
	}
	return p
}

func infoOf(t *testing.T, ctx *Ctx, args ...string) parsedInfo {
	t.Helper()
	raw := Dispatch(ctx, dispatchArgs(append([]string{"INFO"}, args...)...)).AppendTo(nil)
	payload, ok := bulkPayload(raw)
	if !ok {
		t.Fatalf("INFO reply is not a bulk string: %q", raw)
	}
	return parseInfo(t, payload)
}

// bulkPayload unwraps $n\r\n<payload>\r\n.
func bulkPayload(raw []byte) (string, bool) {
	s := string(raw)
	header, rest, ok := strings.Cut(s, "\r\n")
	if !ok || !strings.HasPrefix(header, "$") {
		return "", false
	}
	n, err := strconv.Atoi(header[1:])
	if err != nil || len(rest) != n+2 || !strings.HasSuffix(rest, "\r\n") {
		return "", false
	}
	return rest[:n], true
}

func TestInfoHasEveryFrozenFieldInOrder(t *testing.T) {
	info := infoOf(t, newCtx())
	if !reflect.DeepEqual(info.sections, infoOrder) {
		t.Fatalf("sections = %v, want %v", info.sections, infoOrder)
	}
	for _, sec := range infoOrder {
		if got := info.keys[sec]; !reflect.DeepEqual(got, infoFields[sec]) {
			t.Errorf("%s fields = %v, want %v", sec, got, infoFields[sec])
		}
	}
	if info.values["redis_version"] != server.Version {
		t.Errorf("redis_version = %q, want %q", info.values["redis_version"], server.Version)
	}
	if info.values["process_id"] != strconv.Itoa(os.Getpid()) {
		t.Errorf("process_id = %q", info.values["process_id"])
	}
	if info.values["role"] != "master" || info.values["last_bgsave_status"] != "ok" {
		t.Errorf("idle defaults wrong: role=%q last_bgsave_status=%q", info.values["role"], info.values["last_bgsave_status"])
	}
	if info.values["db0"] != "keys=0,expires=0" {
		t.Errorf("db0 on an empty store = %q, want keys=0,expires=0", info.values["db0"])
	}
}

func TestInfoSectionSelection(t *testing.T) {
	ctx := newCtx()
	cases := []struct {
		args []string
		want []string
	}{
		{[]string{"stats"}, []string{"Stats"}},
		{[]string{"KEYSPACE"}, []string{"Keyspace"}},
		{[]string{"server", "replication"}, []string{"Server", "Replication"}},
		{[]string{"all"}, infoOrder},
		{[]string{"default"}, infoOrder},
		{[]string{"everything"}, infoOrder},
	}
	for _, tc := range cases {
		if got := infoOf(t, ctx, tc.args...).sections; !reflect.DeepEqual(got, tc.want) {
			t.Errorf("INFO %v sections = %v, want %v", tc.args, got, tc.want)
		}
	}
	if got := reply(t, ctx, "INFO", "nosuchsection"); got != "$0\r\n\r\n" {
		t.Errorf("INFO nosuchsection = %q, want an empty bulk", got)
	}
}

// The counters move with the work they describe.
func TestInfoCountersTrackTheKeyspace(t *testing.T) {
	ctx, clock := newCtxWithClock()
	reply(t, ctx, "SET", "a", "1")
	reply(t, ctx, "SET", "b", "2")
	reply(t, ctx, "EXPIRE", "b", "1")
	reply(t, ctx, "GET", "a")
	reply(t, ctx, "GET", "a")
	reply(t, ctx, "GET", "missing")
	reply(t, ctx, "KEYS", "*")

	info := infoOf(t, ctx)
	want := map[string]string{
		"db0":                   "keys=2,expires=1",
		"keyspace_hits":         "2",
		"keyspace_misses":       "1",
		"aof_current_seq":       "3",
		"expired_keys":          "0",
		"keys_last_reply_bytes": strconv.Itoa(len("*2\r\n$1\r\na\r\n$1\r\nb\r\n")),
	}
	for k, v := range want {
		if info.values[k] != v {
			t.Errorf("%s = %q, want %q", k, info.values[k], v)
		}
	}

	advance(ctx, clock, 2*time.Second)
	reply(t, ctx, "GET", "b")
	info = infoOf(t, ctx)
	for k, v := range map[string]string{
		"db0": "keys=1,expires=0", "expired_keys": "1", "keyspace_misses": "2", "uptime_in_seconds": "2",
	} {
		if info.values[k] != v {
			t.Errorf("after expiry %s = %q, want %q", k, info.values[k], v)
		}
	}
}

// A subsystem registers its own fields; they replace defaults with the same
// key and append new ones, without INFO importing the subsystem.
func TestInfoProvidersOverrideAndExtend(t *testing.T) {
	ctx := newCtx()
	ctx.Srv.RegisterInfo("Persistence", func() []server.InfoField {
		return []server.InfoField{
			{Key: "aof_fsync_policy", Value: "always"},
			{Key: "aof_rewrite_scheduled", Value: "0"},
		}
	})
	info := infoOf(t, ctx, "persistence")
	if info.values["aof_fsync_policy"] != "always" {
		t.Errorf("aof_fsync_policy = %q, want the provider's value", info.values["aof_fsync_policy"])
	}
	keys := info.keys["Persistence"]
	if keys[0] != "aof_fsync_policy" || keys[len(keys)-1] != "aof_rewrite_scheduled" {
		t.Errorf("override moved a field or did not append: %v", keys)
	}
	if len(keys) != len(infoFields["Persistence"])+1 {
		t.Errorf("Persistence has %d fields, want %d", len(keys), len(infoFields["Persistence"])+1)
	}
}

// INFO is a read: it never propagates and never counts as a lookup.
func TestInfoIsAPureRead(t *testing.T) {
	ctx := newCtx()
	mem := withAppender(ctx)
	reply(t, ctx, "INFO")
	if len(mem.recs) != 0 || ctx.Srv.Stats.KeyspaceHits+ctx.Srv.Stats.KeyspaceMisses != 0 {
		t.Fatal("INFO propagated or counted a lookup")
	}
}

// Over a real loop: INFO reports the bound port and live client counts, and
// db0 reaches keys=0 once the sweep has reclaimed keys nobody touches again
// (the T3.02 acceptance check, now through INFO).
func TestInfoOverTheLoop(t *testing.T) {
	addr, _ := startLoop(t, nil)
	c := dialLoop(t, addr)
	other := dialLoop(t, addr)
	other.do("PING")

	info := parseInfo(t, c.do("INFO"))
	_, port, _ := strings.Cut(addr, ":")
	if info.values["tcp_port"] != port {
		t.Errorf("tcp_port = %q, want %q", info.values["tcp_port"], port)
	}
	if info.values["connected_clients"] != "2" {
		t.Errorf("connected_clients = %q, want 2", info.values["connected_clients"])
	}

	const n = 2000
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "SET k%d v\r\nEXPIRE k%d 1\r\n", i, i)
	}
	c.send(b.String())
	for i := 0; i < 2*n; i++ {
		c.read()
	}
	if got := parseInfo(t, c.do("INFO keyspace")).values["db0"]; got != fmt.Sprintf("keys=%d,expires=%d", n, n) {
		t.Fatalf("db0 after load = %q", got)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		info := parseInfo(t, c.do("INFO"))
		if info.values["db0"] == "keys=0,expires=0" {
			if info.values["total_sweep_deleted"] != strconv.Itoa(n) {
				t.Fatalf("total_sweep_deleted = %q, want %d", info.values["total_sweep_deleted"], n)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("db0 still %q after 5 s", info.values["db0"])
		}
		time.Sleep(50 * time.Millisecond)
	}
}
