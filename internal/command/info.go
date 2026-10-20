package command

import (
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/SVIGHNESH/RedForge/internal/resp"
	"github.com/SVIGHNESH/RedForge/internal/server"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

func init() {
	register(&Command{Name: "INFO", Arity: -1, Flags: ReadOnly, Handler: infoCommand})
}

// infoSection is one "# Name" block of INFO output. fields builds the
// default lines; providers registered on the server for the same name can
// override or extend them.
type infoSection struct {
	name   string
	fields func(ctx *Ctx) []server.InfoField
}

// infoSections is the frozen field list from MASTER-PLAN Section 4.10, in
// output order. A field whose subsystem has not merged yet is still emitted,
// with its idle value, so tests can rely on a stable schema; the subsystem
// replaces it through Server.RegisterInfo when it lands.
var infoSections = []infoSection{
	{"Server", serverInfo},
	{"Clients", clientsInfo},
	{"Memory", memoryInfo},
	{"Stats", statsInfo},
	{"Persistence", persistenceInfo},
	{"Replication", replicationInfo},
	{"Keyspace", keyspaceInfo},
}

// INFO [section ...] returns the Redis section format: "# Section" headers,
// key:value lines, CRLF line endings and a blank line between sections, so
// redis-cli INFO and redis-cli --stat render it. With no argument, or with
// "default", "all" or "everything", every section is returned. Section names
// are case-insensitive, and an unknown one contributes nothing, as in Redis.
func infoCommand(ctx *Ctx, args [][]byte) resp.Reply {
	want := map[string]bool{}
	all := len(args) == 1
	for _, a := range args[1:] {
		name := strings.ToLower(string(a))
		switch name {
		case "default", "all", "everything":
			all = true
		default:
			want[name] = true
		}
	}

	var b strings.Builder
	for _, sec := range infoSections {
		if !all && !want[strings.ToLower(sec.name)] {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\r\n")
		}
		b.WriteString("# ")
		b.WriteString(sec.name)
		b.WriteString("\r\n")
		for _, f := range mergeInfo(sec.fields(ctx), ctx.Srv.InfoProviders(sec.name)) {
			b.WriteString(f.Key)
			b.WriteByte(':')
			b.WriteString(f.Value)
			b.WriteString("\r\n")
		}
	}
	return resp.BulkString(b.String())
}

// mergeInfo overlays provider fields on the defaults: a provider field with
// a default's key replaces its value in place, and any new key is appended.
func mergeInfo(defaults []server.InfoField, providers []server.InfoProvider) []server.InfoField {
	if len(providers) == 0 {
		return defaults
	}
	index := make(map[string]int, len(defaults))
	for i, f := range defaults {
		index[f.Key] = i
	}
	for _, p := range providers {
		for _, f := range p() {
			if i, ok := index[f.Key]; ok {
				defaults[i].Value = f.Value
				continue
			}
			index[f.Key] = len(defaults)
			defaults = append(defaults, f)
		}
	}
	return defaults
}

// countLookup records a keyspace_hits or keyspace_misses for a read that
// looked a key up. T3.05 keeps the set small: GET, HGET and LRANGE count,
// other commands do not.
func countLookup(ctx *Ctx, found bool) {
	if found {
		ctx.Srv.Stats.KeyspaceHits++
	} else {
		ctx.Srv.Stats.KeyspaceMisses++
	}
}

func field(key string, n int64) server.InfoField {
	return server.InfoField{Key: key, Value: strconv.FormatInt(n, 10)}
}

func serverInfo(ctx *Ctx) []server.InfoField {
	uptime := (ctx.Now - ctx.Srv.StartMs()) / 1000
	if uptime < 0 {
		uptime = 0
	}
	return []server.InfoField{
		{Key: "redis_version", Value: server.Version},
		field("uptime_in_seconds", uptime),
		field("tcp_port", int64(ctx.Srv.TCPPort())),
		field("process_id", int64(os.Getpid())),
		// goroutines is what the 100-connection test (T1.04) samples to
		// prove connections add no goroutines (MASTER-PLAN P1.3).
		field("goroutines", int64(runtime.NumGoroutine())),
	}
}

// clientsInfo also reports blocked_clients, always 0 because no command in
// the matrix blocks; redis-cli --stat reads it.
func clientsInfo(ctx *Ctx) []server.InfoField {
	return []server.InfoField{
		field("connected_clients", ctx.Srv.ConnectedClients()),
		field("blocked_clients", 0),
	}
}

// memoryInfo is not in the frozen list, but redis-cli --stat shows
// used_memory and prints garbage without it. It reports the Go heap in use.
// ReadMemStats briefly stops the world, which is acceptable for a command
// as rare as INFO and is noted for the evaluation.
func memoryInfo(*Ctx) []server.InfoField {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return []server.InfoField{field("used_memory", int64(m.HeapAlloc))}
}

func statsInfo(ctx *Ctx) []server.InfoField {
	var expired, swept int64
	if c, ok := ctx.Store.(store.Counters); ok {
		expired, swept = c.ExpiredKeys(), c.SweepDeleted()
	}
	st := &ctx.Srv.Stats
	return []server.InfoField{
		field("total_connections_received", ctx.Srv.TotalConnections()),
		field("total_commands_processed", ctx.Srv.TotalCommands()),
		field("expired_keys", expired),
		field("total_sweep_deleted", swept),
		field("keyspace_hits", st.KeyspaceHits),
		field("keyspace_misses", st.KeyspaceMisses),
		field("keys_last_iter_us", st.KeysLastIterUs),
		field("keys_last_reply_bytes", st.KeysLastReplyBytes),
	}
}

// persistenceInfo reports the sequence counter, which exists today, and the
// idle values of the log (T5.x) and BGSAVE (T6.x) fields until those
// subsystems register their own providers. aof_fsync_policy shows the
// configured default from MASTER-PLAN Section 3.5.
func persistenceInfo(ctx *Ctx) []server.InfoField {
	return []server.InfoField{
		{Key: "aof_fsync_policy", Value: "everysec"},
		{Key: "aof_current_seq", Value: strconv.FormatUint(ctx.Srv.CurrentSeq(), 10)},
		// -1 means replay found no torn or corrupt record (T5.03).
		field("aof_last_truncation_offset", -1),
		field("aof_truncated_records", 0),
		field("bgsave_in_progress", 0),
		{Key: "last_bgsave_status", Value: "ok"},
		field("last_bgsave_seq", 0),
		field("last_bgsave_clone_ms", 0),
	}
}

// replicationInfo is the primary's view until S1 registers a provider.
func replicationInfo(*Ctx) []server.InfoField {
	return []server.InfoField{
		{Key: "role", Value: "master"},
		field("connected_replicas", 0),
	}
}

// keyspaceInfo always emits db0, even when empty, so tests and the EX4
// check can poll for db0:keys=0. keys counts every stored key, including an
// expired one the sweep has not reached yet, as Redis does.
func keyspaceInfo(ctx *Ctx) []server.InfoField {
	v := "keys=" + strconv.Itoa(ctx.Store.Len()) + ",expires=" + strconv.Itoa(ctx.Store.ExpiringLen())
	return []server.InfoField{{Key: "db0", Value: v}}
}
