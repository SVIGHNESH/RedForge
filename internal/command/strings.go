package command

import (
	"github.com/SVIGHNESH/RedForge/internal/resp"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

func init() {
	register(&Command{Name: "SET", Arity: 3, Flags: Write, Handler: setCommand})
	register(&Command{Name: "GET", Arity: 2, Flags: ReadOnly, Handler: getCommand})
}

// SET k v replaces whatever k held, of any type, and clears any TTL, as Redis
// does. Options such as EX and NX are outside the command matrix, so the
// fixed arity rejects them with the wrong-arguments error.
func setCommand(ctx *Ctx, args [][]byte) resp.Reply {
	// args alias the client's read buffer, which the loop reuses for the
	// next frame, so the stored value must own its bytes.
	val := make([]byte, len(args[2]))
	copy(val, args[2])
	ctx.Store.Put(string(args[1]), &store.Entry{Type: store.TString, Val: &store.StringVal{B: val}})
	ctx.Propagate(args[0], args[1], args[2])
	return resp.OK
}

// GET k returns the string at k, nil when k is missing, and WRONGTYPE when k
// holds a collection. Passive expiry happens inside Store.Lookup.
func getCommand(ctx *Ctx, args [][]byte) resp.Reply {
	e, ok := ctx.Store.Lookup(string(args[1]))
	if !ok {
		return resp.Nil
	}
	if e.Type != store.TString {
		return errWrongType
	}
	return resp.Bulk(e.Val.(*store.StringVal).B)
}
