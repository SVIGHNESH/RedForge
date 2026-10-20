package command

import (
	"strconv"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/resp"
)

func init() {
	register(&Command{Name: "DEL", Arity: -2, Flags: Write, Handler: delCommand})
	register(&Command{Name: "EXISTS", Arity: -2, Flags: ReadOnly, Handler: existsCommand})
	register(&Command{Name: "KEYS", Arity: 2, Flags: ReadOnly, Handler: keysCommand})
}

// DEL k [k ...] returns how many keys it removed. Only the keys that existed
// are propagated, and a DEL that removed nothing propagates nothing, so the
// log never carries a no-op (MASTER-PLAN Section 4.4).
func delCommand(ctx *Ctx, args [][]byte) resp.Reply {
	removed := [][]byte{args[0]}
	for _, k := range args[1:] {
		if ctx.Store.Delete(string(k)) {
			removed = append(removed, k)
		}
	}
	n := len(removed) - 1
	if n > 0 {
		ctx.Propagate(removed...)
	}
	return resp.Integer(int64(n))
}

// EXISTS k [k ...] counts every argument that names a live key. A key named
// twice is counted twice, as Redis does.
func existsCommand(ctx *Ctx, args [][]byte) resp.Reply {
	var n int64
	for _, k := range args[1:] {
		if ctx.Store.Exists(string(k)) {
			n++
		}
	}
	return resp.Integer(n)
}

// KEYS pattern returns every live key matching the Redis glob, in map order
// as Redis does (it does not sort). Store.Keys reclaims expired keys it walks
// past, which is passive expiry applied during enumeration.
//
// KEYS blocks the loop for the whole walk, which is exactly what the stall
// measurement in T8.05 wants to see, so the walk time and the reply size are
// recorded separately for INFO.
func keysCommand(ctx *Ctx, args [][]byte) resp.Reply {
	start := time.Now()
	keys := ctx.Store.Keys(string(args[1]))
	ctx.Srv.Stats.KeysLastIterUs = time.Since(start).Microseconds()

	items := make([][]byte, len(keys))
	for i, k := range keys {
		items[i] = []byte(k)
	}
	ctx.Srv.Stats.KeysLastReplyBytes = bulkArraySize(items)
	return resp.ArrayOfBulks(items)
}

// bulkArraySize is the encoded length of an array of bulk strings, computed
// without serialising it twice: *N\r\n then $len\r\nbytes\r\n per item.
func bulkArraySize(items [][]byte) int64 {
	n := 1 + len(strconv.Itoa(len(items))) + 2
	for _, it := range items {
		n += 1 + len(strconv.Itoa(len(it))) + 2 + len(it) + 2
	}
	return int64(n)
}
