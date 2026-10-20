package command

import (
	"github.com/SVIGHNESH/RedForge/internal/resp"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

func init() {
	register(&Command{Name: "SADD", Arity: -3, Flags: Write, Handler: saddCommand})
	register(&Command{Name: "SREM", Arity: -3, Flags: Write, Handler: sremCommand})
	register(&Command{Name: "SISMEMBER", Arity: 3, Flags: ReadOnly, Handler: sismemberCommand})
	register(&Command{Name: "SMEMBERS", Arity: 2, Flags: ReadOnly, Handler: smembersCommand})
	register(&Command{Name: "SINTER", Arity: -2, Flags: ReadOnly, Handler: sinterCommand})
}

// setWrongType is the MASTER-PLAN 4.11 wording. It is local to the set
// handlers so this file does not depend on the string commands landing
// first; the copies can be folded into one shared constant later.
var setWrongType = resp.Error("WRONGTYPE Operation against a key holding the wrong kind of value")

// setLookup returns the set stored at key for reading. A missing key is
// (nil, nil); a key of another type is (nil, WRONGTYPE).
func setLookup(ctx *Ctx, key []byte) (*store.SetVal, resp.Reply) {
	e, ok := ctx.Store.Lookup(string(key))
	if !ok {
		return nil, nil
	}
	if e.Type != store.TSet {
		return nil, setWrongType
	}
	return e.Val.(*store.SetVal), nil
}

// setMutable is setLookup for writes: it goes through Store.Mutable, the
// copy-on-write point for snapshots (MASTER-PLAN 4.6).
func setMutable(ctx *Ctx, key []byte) (*store.SetVal, resp.Reply) {
	e, ok := ctx.Store.Mutable(string(key))
	if !ok {
		return nil, nil
	}
	if e.Type != store.TSet {
		return nil, setWrongType
	}
	return e.Val.(*store.SetVal), nil
}

// SADD k m [m ...] returns how many members were actually added. Only those
// members propagate, so a repeat SADD of existing members writes no record.
func saddCommand(ctx *Ctx, args [][]byte) resp.Reply {
	s, errReply := setMutable(ctx, args[1])
	if errReply != nil {
		return errReply
	}
	if s == nil {
		s = &store.SetVal{M: make(map[string]struct{}, len(args)-2)}
		ctx.Store.Put(string(args[1]), &store.Entry{Type: store.TSet, Val: s})
	}
	added := [][]byte{args[0], args[1]}
	for _, m := range args[2:] {
		// string(m) copies, so the member does not alias the read buffer.
		member := string(m)
		if _, ok := s.M[member]; ok {
			continue
		}
		s.M[member] = struct{}{}
		added = append(added, m)
	}
	n := len(added) - 2
	if n > 0 {
		ctx.Propagate(added...)
	}
	return resp.Integer(int64(n))
}

// SREM k m [m ...] returns how many members were removed. An emptied set is
// deleted as a key. Only removed members propagate, and nothing when none
// were.
func sremCommand(ctx *Ctx, args [][]byte) resp.Reply {
	s, errReply := setMutable(ctx, args[1])
	if errReply != nil {
		return errReply
	}
	if s == nil {
		return resp.Integer(0)
	}
	removed := [][]byte{args[0], args[1]}
	for _, m := range args[2:] {
		member := string(m)
		if _, ok := s.M[member]; !ok {
			continue
		}
		delete(s.M, member)
		removed = append(removed, m)
	}
	n := len(removed) - 2
	if n == 0 {
		return resp.Integer(0)
	}
	if len(s.M) == 0 {
		ctx.Store.Delete(string(args[1]))
	}
	ctx.Propagate(removed...)
	return resp.Integer(int64(n))
}

// SISMEMBER k m returns 1 when m is in the set, 0 otherwise or when k is
// missing.
func sismemberCommand(ctx *Ctx, args [][]byte) resp.Reply {
	s, errReply := setLookup(ctx, args[1])
	if errReply != nil {
		return errReply
	}
	if s == nil {
		return resp.Integer(0)
	}
	if _, ok := s.M[string(args[2])]; ok {
		return resp.Integer(1)
	}
	return resp.Integer(0)
}

// SMEMBERS k returns every member in map order, which Redis also leaves
// unspecified. A missing key is an empty array.
func smembersCommand(ctx *Ctx, args [][]byte) resp.Reply {
	s, errReply := setLookup(ctx, args[1])
	if errReply != nil {
		return errReply
	}
	if s == nil {
		return resp.EmptyArray()
	}
	out := make([][]byte, 0, len(s.M))
	for m := range s.M {
		out = append(out, []byte(m))
	}
	return resp.ArrayOfBulks(out)
}

// SINTER k1 [k2 ...] returns the members present in every set. A missing key
// is an empty set, so the result is empty; a key of another type is
// WRONGTYPE, checked for every key first as Redis does. The smallest set is
// iterated and the others probed, so the cost is O(smallest * keys).
func sinterCommand(ctx *Ctx, args [][]byte) resp.Reply {
	sets := make([]*store.SetVal, 0, len(args)-1)
	missing := false
	for _, key := range args[1:] {
		s, errReply := setLookup(ctx, key)
		if errReply != nil {
			return errReply
		}
		if s == nil {
			missing = true
			continue
		}
		sets = append(sets, s)
	}
	if missing {
		return resp.EmptyArray()
	}
	smallest := 0
	for i, s := range sets {
		if len(s.M) < len(sets[smallest].M) {
			smallest = i
		}
	}
	out := [][]byte{}
	for m := range sets[smallest].M {
		inAll := true
		for i, s := range sets {
			if i == smallest {
				continue
			}
			if _, ok := s.M[m]; !ok {
				inAll = false
				break
			}
		}
		if inAll {
			out = append(out, []byte(m))
		}
	}
	return resp.ArrayOfBulks(out)
}
