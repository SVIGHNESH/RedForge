package command

import "github.com/SVIGHNESH/RedForge/internal/resp"

func init() {
	// ReadOnly: BGSAVE never changes the keyspace and never propagates.
	register(&Command{Name: "BGSAVE", Arity: 1, Flags: ReadOnly | Admin | NoTx, Handler: bgsaveCommand})
}

var bgsaveStarted = resp.SimpleString("Background saving started")

// BGSAVE starts a snapshot without blocking the loop for the write
// (MASTER-PLAN Section 4.6). On the loop thread it takes the sequence
// boundary and the store's point-in-time view, then hands both to the
// writer goroutine; copy-on-write in Store.Mutable keeps the view stable
// while clients keep writing. A second BGSAVE while one runs is refused.
// BGSAVE writes nothing to the log, so it never propagates.
func bgsaveCommand(ctx *Ctx, _ [][]byte) resp.Reply {
	saver := ctx.Srv.BackgroundSaver()
	if saver == nil {
		return resp.Error("ERR BGSAVE is not available: no persistence directory configured")
	}
	if saver.InProgress() {
		return resp.Error("ERR Background save already in progress")
	}
	boundary := ctx.Srv.CurrentSeq()
	if err := saver.Start(boundary, ctx.Store.Snapshot()); err != nil {
		return resp.Error("ERR " + err.Error())
	}
	return bgsaveStarted
}
