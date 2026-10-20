package command

import (
	"github.com/SVIGHNESH/RedForge/internal/resp"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

func init() {
	register(&Command{Name: "LPUSH", Arity: -3, Flags: Write, Handler: lpushCommand})
	register(&Command{Name: "RPUSH", Arity: -3, Flags: Write, Handler: rpushCommand})
	register(&Command{Name: "LPOP", Arity: 2, Flags: Write, Handler: lpopCommand})
	register(&Command{Name: "RPOP", Arity: 2, Flags: Write, Handler: rpopCommand})
	register(&Command{Name: "LLEN", Arity: 2, Flags: ReadOnly, Handler: llenCommand})
	register(&Command{Name: "LRANGE", Arity: 4, Flags: ReadOnly, Handler: lrangeCommand})
}

// listForRead returns the list stored at key for a read command. A missing
// key is (nil, nil); a key of another type is (nil, WRONGTYPE). The lookup
// counts toward keyspace_hits or keyspace_misses, as Redis counts every read
// lookup, including one that then fails the type check.
func listForRead(ctx *Ctx, key []byte) (*store.ListVal, resp.Reply) {
	e, ok := ctx.Store.Lookup(string(key))
	countLookup(ctx, ok)
	if !ok {
		return nil, nil
	}
	if e.Type != store.TList {
		return nil, errWrongType
	}
	return e.Val.(*store.ListVal), nil
}

// listForWrite returns the list at key through Store.Mutable, the
// copy-on-write point for snapshots (MASTER-PLAN 4.6). When create is set a
// missing key gets a new empty list; otherwise a missing key is (nil, nil).
func listForWrite(ctx *Ctx, key []byte, create bool) (*store.ListVal, resp.Reply) {
	e, ok := ctx.Store.Mutable(string(key))
	if !ok {
		if !create {
			return nil, nil
		}
		l := &store.ListVal{}
		ctx.Store.Put(string(key), &store.Entry{Type: store.TList, Val: l})
		return l, nil
	}
	if e.Type != store.TList {
		return nil, errWrongType
	}
	return e.Val.(*store.ListVal), nil
}

// copyBytes detaches a value from the client's read buffer, which the loop
// reuses for the next command.
func copyBytes(b []byte) []byte {
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

// LPUSH k v [v ...] pushes each value at the head in argument order, so
// LPUSH k a b c reads c b a. It replies with the new length and propagates
// as received, since replay rebuilds the same list.
func lpushCommand(ctx *Ctx, args [][]byte) resp.Reply {
	return pushCommand(ctx, args, true)
}

// RPUSH k v [v ...] appends each value at the tail in argument order.
func rpushCommand(ctx *Ctx, args [][]byte) resp.Reply {
	return pushCommand(ctx, args, false)
}

func pushCommand(ctx *Ctx, args [][]byte, front bool) resp.Reply {
	l, errReply := listForWrite(ctx, args[1], true)
	if errReply != nil {
		return errReply
	}
	for _, v := range args[2:] {
		if front {
			l.PushFront(copyBytes(v))
		} else {
			l.PushBack(copyBytes(v))
		}
	}
	ctx.Propagate(args...)
	return resp.Integer(int64(l.Len()))
}

// LPOP k removes and returns the head. A missing key is nil and does not
// propagate. A pop that empties the list deletes the key, so no empty list
// is ever visible to EXISTS, KEYS or a snapshot.
func lpopCommand(ctx *Ctx, args [][]byte) resp.Reply {
	return popCommand(ctx, args, true)
}

// RPOP k removes and returns the tail.
func rpopCommand(ctx *Ctx, args [][]byte) resp.Reply {
	return popCommand(ctx, args, false)
}

func popCommand(ctx *Ctx, args [][]byte, front bool) resp.Reply {
	l, errReply := listForWrite(ctx, args[1], false)
	if errReply != nil {
		return errReply
	}
	if l == nil {
		return resp.Nil
	}
	var v []byte
	var ok bool
	if front {
		v, ok = l.PopFront()
	} else {
		v, ok = l.PopBack()
	}
	if !ok {
		// An empty list is never stored; if one ever is, drop it.
		ctx.Store.Delete(string(args[1]))
		return resp.Nil
	}
	if l.Len() == 0 {
		ctx.Store.Delete(string(args[1]))
	}
	ctx.Propagate(args[0], args[1])
	return resp.Bulk(v)
}

// LLEN k is the list length, 0 for a missing key.
func llenCommand(ctx *Ctx, args [][]byte) resp.Reply {
	l, errReply := listForRead(ctx, args[1])
	if errReply != nil {
		return errReply
	}
	if l == nil {
		return resp.Integer(0)
	}
	return resp.Integer(int64(l.Len()))
}

// LRANGE k start stop returns elements start..stop inclusive with Redis's
// negative-index and clamping rules (ListVal.Range). A missing key is an
// empty array. Bad integers are rejected before the key is looked at, as in
// Redis.
func lrangeCommand(ctx *Ctx, args [][]byte) resp.Reply {
	start, ok1 := parseInt64(args[2])
	stop, ok2 := parseInt64(args[3])
	if !ok1 || !ok2 {
		return errNotInt
	}
	l, errReply := listForRead(ctx, args[1])
	if errReply != nil {
		return errReply
	}
	if l == nil {
		return resp.EmptyArray()
	}
	return resp.ArrayOfBulks(l.Range(clampIndex(start), clampIndex(stop)))
}

// clampIndex narrows an int64 index to int without wrapping. Any index past
// the int range is already past either end of any list, so clamping keeps
// the Range result identical.
func clampIndex(n int64) int {
	const maxInt = int64(^uint(0) >> 1)
	if n > maxInt {
		return int(maxInt)
	}
	if n < -maxInt {
		return int(-maxInt)
	}
	return int(n)
}
