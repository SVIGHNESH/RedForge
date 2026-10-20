package command

import (
	"math"
	"strconv"

	"github.com/SVIGHNESH/RedForge/internal/resp"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

func init() {
	register(&Command{Name: "SET", Arity: 3, Flags: Write, Handler: setCommand})
	register(&Command{Name: "GET", Arity: 2, Flags: ReadOnly, Handler: getCommand})
	register(&Command{Name: "INCR", Arity: 2, Flags: Write, Handler: incrCommand})
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
// holds a collection. Passive expiry happens inside Store.Lookup. Each GET
// counts as a keyspace hit or miss for INFO, as Redis counts its lookups.
func getCommand(ctx *Ctx, args [][]byte) resp.Reply {
	e, ok := ctx.Store.Lookup(string(args[1]))
	countLookup(ctx, ok)
	if !ok {
		return resp.Nil
	}
	if e.Type != store.TString {
		return errWrongType
	}
	return resp.Bulk(e.Val.(*store.StringVal).B)
}

// INCR k adds one to the integer stored at k, treating a missing key as 0.
// The value must parse under Redis's integer rules (see parseInt64), and an
// increment past math.MaxInt64 is refused rather than wrapped.
//
// The old StringVal is never mutated: a new entry is stored, keeping the
// existing TTL as Redis does. Mutating in place would leak the new value
// into a snapshot that shares the old entry (MASTER-PLAN Section 4.6).
//
// INCR k propagates itself rather than SET k <new>: replay recomputes the
// same value from the same starting state and the log stays small.
func incrCommand(ctx *Ctx, args [][]byte) resp.Reply {
	key := string(args[1])
	var cur, expireAt int64
	if e, ok := ctx.Store.Lookup(key); ok {
		if e.Type != store.TString {
			return errWrongType
		}
		n, ok := parseInt64(e.Val.(*store.StringVal).B)
		if !ok {
			return errNotInt
		}
		cur, expireAt = n, e.ExpireAt
	}
	if cur == math.MaxInt64 {
		return errOverflow
	}
	next := cur + 1
	ctx.Store.Put(key, &store.Entry{
		Type:     store.TString,
		Val:      &store.StringVal{B: strconv.AppendInt(nil, next, 10)},
		ExpireAt: expireAt,
	})
	ctx.Propagate(args[0], args[1])
	return resp.Integer(next)
}
