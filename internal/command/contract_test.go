package command

import (
	"testing"

	"github.com/SVIGHNESH/RedForge/internal/resp"
	"github.com/SVIGHNESH/RedForge/internal/server"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

// TestCommandContractCompiles pins the frozen handler, Ctx and Command shapes
// (docs/CONTRACTS.md Section 4.4) so a signature change fails to compile.
func TestCommandContractCompiles(t *testing.T) {
	var (
		_ func(*Ctx, [][]byte) resp.Reply                = Handler(nil)
		_ func(*Ctx, ...[]byte)                          = (*Ctx).Propagate
		_ func(*Ctx, [][]byte) resp.Reply                = Dispatch
		_ func(*server.Server, store.Store, store.Clock) = Install
		_ func(server.Appender, uint64, [][]byte) error  = server.Appender.Append
		_ func(*server.Server) uint64                    = (*server.Server).NextSeq
		_ func(*server.Server) uint64                    = (*server.Server).CurrentSeq
		_ func(*server.Server, uint64)                   = (*server.Server).SetSeq
	)
	ctx := Ctx{Client: (*server.Client)(nil), Store: store.Store(nil), Srv: (*server.Server)(nil), Now: int64(0)}
	cmd := Command{Name: "", Arity: 0, Flags: Write | ReadOnly | Admin | PubSub | NoTx, Handler: Handler(nil)}
	if ctx.Now != 0 || cmd.Arity != 0 {
		t.Fatal("unreachable")
	}
}
