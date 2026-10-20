package command

import (
	"math"
	"strconv"

	"github.com/SVIGHNESH/RedForge/internal/resp"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

func init() {
	register(&Command{Name: "EXPIRE", Arity: 3, Flags: Write, Handler: expireCommand})
	// PEXPIREAT is the logged form of EXPIRE. It is accepted from clients
	// because rejecting a record the server itself writes would be strange,
	// but it is not advertised (MASTER-PLAN Section 1.2).
	register(&Command{Name: "PEXPIREAT", Arity: 3, Flags: Write, Handler: pexpireatCommand})
	register(&Command{Name: "TTL", Arity: 2, Flags: ReadOnly, Handler: ttlCommand})
	register(&Command{Name: "PERSIST", Arity: 2, Flags: Write, Handler: persistCommand})
}

var pexpireatName = []byte("PEXPIREAT")

// EXPIRE k seconds turns the relative TTL into an absolute Unix millisecond
// timestamp at once, read from the command's single clock reading. Only that
// absolute form ever reaches the log or a replica, as PEXPIREAT k <ms>, so a
// restart can never stretch a key's lifetime (Proposal Section 6.3).
func expireCommand(ctx *Ctx, args [][]byte) resp.Reply {
	secs, ok := parseInt64(args[2])
	if !ok {
		return errNotInt
	}
	// Redis's own overflow guard: seconds to ms, then adding now, must both
	// stay inside int64.
	if secs > math.MaxInt64/1000 || secs < math.MinInt64/1000 {
		return invalidExpire("expire")
	}
	ms := secs * 1000
	if ms > math.MaxInt64-ctx.Now {
		return invalidExpire("expire")
	}
	return setExpiry(ctx, args[1], ctx.Now+ms)
}

// PEXPIREAT k ms sets an absolute expiry directly.
func pexpireatCommand(ctx *Ctx, args [][]byte) resp.Reply {
	at, ok := parseInt64(args[2])
	if !ok {
		return errNotInt
	}
	return setExpiry(ctx, args[1], at)
}

// setExpiry is the shared tail of EXPIRE and PEXPIREAT. It answers 1 when
// the key exists and 0 otherwise. A time at or before now deletes the key
// immediately and propagates DEL, as Redis does; anything later is stored
// and propagated as PEXPIREAT k <at>, whichever command the client sent.
func setExpiry(ctx *Ctx, key []byte, at int64) resp.Reply {
	k := string(key)
	if !ctx.Store.Exists(k) {
		return resp.Integer(0)
	}
	// During replay a past deadline is stored, not acted on: recovery drops
	// due keys in one pass afterwards and counts them as expired_keys.
	if at <= ctx.Now && !ctx.replaying {
		ctx.Store.Delete(k)
		ctx.Propagate([]byte("DEL"), key)
		return resp.Integer(1)
	}
	ctx.Store.SetExpireAt(k, at)
	ctx.Propagate(pexpireatName, key, strconv.AppendInt(nil, at, 10))
	return resp.Integer(1)
}

func invalidExpire(cmd string) resp.Reply {
	return resp.Error("ERR invalid expire time in '" + cmd + "' command")
}

// TTL k answers -2 for a missing key, -1 for a key without expiry, and
// otherwise the remaining seconds rounded to the nearest second, which is
// what Redis computes ((ms + 500) / 1000), so a fresh EXPIRE k 100 reads 100.
func ttlCommand(ctx *Ctx, args [][]byte) resp.Reply {
	ms, status := ctx.Store.TTLms(string(args[1]))
	switch status {
	case store.NoKey:
		return resp.Integer(-2)
	case store.NoExpire:
		return resp.Integer(-1)
	}
	return resp.Integer((ms + 500) / 1000)
}

// PERSIST k removes the TTL. It answers 1 and propagates itself only when a
// TTL was actually removed; on a key without one it is a no-op and is not
// logged.
func persistCommand(ctx *Ctx, args [][]byte) resp.Reply {
	if !ctx.Store.Persist(string(args[1])) {
		return resp.Integer(0)
	}
	ctx.Propagate(args[0], args[1])
	return resp.Integer(1)
}
