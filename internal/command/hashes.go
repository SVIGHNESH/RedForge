package command

import (
	"github.com/SVIGHNESH/RedForge/internal/resp"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

func init() {
	register(&Command{Name: "HSET", Arity: -4, Flags: Write, Handler: hsetCommand})
	register(&Command{Name: "HGET", Arity: 3, Flags: ReadOnly, Handler: hgetCommand})
	register(&Command{Name: "HDEL", Arity: -3, Flags: Write, Handler: hdelCommand})
	register(&Command{Name: "HLEN", Arity: 2, Flags: ReadOnly, Handler: hlenCommand})
	register(&Command{Name: "HGETALL", Arity: 2, Flags: ReadOnly, Handler: hgetallCommand})
}

// hashWrongType is the MASTER-PLAN 4.11 wording. It is local to the hash
// handlers so this file does not depend on the string commands landing
// first; the copies can be folded into one shared constant later.
var hashWrongType = resp.Error("WRONGTYPE Operation against a key holding the wrong kind of value")

// hashLookup returns the hash stored at key for reading. A missing key is
// (nil, nil); a key of another type is (nil, WRONGTYPE).
func hashLookup(ctx *Ctx, key []byte) (*store.HashVal, resp.Reply) {
	e, ok := ctx.Store.Lookup(string(key))
	if !ok {
		return nil, nil
	}
	if e.Type != store.THash {
		return nil, hashWrongType
	}
	return e.Val.(*store.HashVal), nil
}

// hashMutable is hashLookup for writes: it goes through Store.Mutable, the
// copy-on-write point for snapshots (MASTER-PLAN 4.6).
func hashMutable(ctx *Ctx, key []byte) (*store.HashVal, resp.Reply) {
	e, ok := ctx.Store.Mutable(string(key))
	if !ok {
		return nil, nil
	}
	if e.Type != store.THash {
		return nil, hashWrongType
	}
	return e.Val.(*store.HashVal), nil
}

// HSET k f v [f v ...] sets each field and returns how many were new.
// Updating an existing field does not count. The command propagates as
// received, because replaying it yields the same hash.
func hsetCommand(ctx *Ctx, args [][]byte) resp.Reply {
	if (len(args)-2)%2 != 0 {
		return resp.Error("ERR wrong number of arguments for 'hset' command")
	}
	h, errReply := hashMutable(ctx, args[1])
	if errReply != nil {
		return errReply
	}
	if h == nil {
		h = &store.HashVal{M: make(map[string][]byte, (len(args)-2)/2)}
		ctx.Store.Put(string(args[1]), &store.Entry{Type: store.THash, Val: h})
	}
	added := 0
	for i := 2; i < len(args); i += 2 {
		field := string(args[i])
		if _, exists := h.M[field]; !exists {
			added++
		}
		// Copy the value: args point into the reused read buffer.
		v := make([]byte, len(args[i+1]))
		copy(v, args[i+1])
		h.M[field] = v
	}
	ctx.Propagate(args...)
	return resp.Integer(int64(added))
}

// HGET k f returns the field's value, or nil when the key or field is missing.
func hgetCommand(ctx *Ctx, args [][]byte) resp.Reply {
	h, errReply := hashLookup(ctx, args[1])
	if errReply != nil {
		return errReply
	}
	if h == nil {
		return resp.Nil
	}
	v, ok := h.M[string(args[2])]
	if !ok {
		return resp.Nil
	}
	return resp.Bulk(v)
}

// HDEL k f [f ...] removes the fields and returns how many existed. A hash
// left with no fields is deleted as a key. Only the fields that existed are
// propagated, and nothing is propagated when none did.
func hdelCommand(ctx *Ctx, args [][]byte) resp.Reply {
	h, errReply := hashMutable(ctx, args[1])
	if errReply != nil {
		return errReply
	}
	if h == nil {
		return resp.Integer(0)
	}
	removed := [][]byte{args[0], args[1]}
	for _, f := range args[2:] {
		field := string(f)
		if _, ok := h.M[field]; ok {
			delete(h.M, field)
			removed = append(removed, f)
		}
	}
	n := len(removed) - 2
	if n == 0 {
		return resp.Integer(0)
	}
	if len(h.M) == 0 {
		ctx.Store.Delete(string(args[1]))
	}
	ctx.Propagate(removed...)
	return resp.Integer(int64(n))
}

// HLEN k returns the field count, 0 for a missing key.
func hlenCommand(ctx *Ctx, args [][]byte) resp.Reply {
	h, errReply := hashLookup(ctx, args[1])
	if errReply != nil {
		return errReply
	}
	if h == nil {
		return resp.Integer(0)
	}
	return resp.Integer(int64(len(h.M)))
}

// HGETALL k returns field, value, field, value... in map order, which Redis
// also leaves unspecified. A missing key is an empty array.
func hgetallCommand(ctx *Ctx, args [][]byte) resp.Reply {
	h, errReply := hashLookup(ctx, args[1])
	if errReply != nil {
		return errReply
	}
	if h == nil {
		return resp.EmptyArray()
	}
	out := make([][]byte, 0, 2*len(h.M))
	for f, v := range h.M {
		out = append(out, []byte(f), v)
	}
	return resp.ArrayOfBulks(out)
}
