package command

import "github.com/SVIGHNESH/RedForge/internal/resp"

func init() {
	register(&Command{Name: "DEL", Arity: -2, Flags: Write, Handler: delCommand})
	register(&Command{Name: "EXISTS", Arity: -2, Flags: ReadOnly, Handler: existsCommand})
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
