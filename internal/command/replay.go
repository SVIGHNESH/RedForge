package command

import (
	"fmt"
	"strings"

	"github.com/SVIGHNESH/RedForge/internal/resp"
	"github.com/SVIGHNESH/RedForge/internal/server"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

// Replay applies one logged command to st during startup recovery (T5.03)
// and, under S1, one record from the primary on a replica. It runs the same
// handler a client would reach through Dispatch, so a record replays exactly
// as it first executed, with three differences:
//
//   - Nothing propagates: the record is already in the log, so appending it
//     again would duplicate it.
//   - No sequence number is consumed: the caller resumes the counter from
//     the last record with Server.SetSeq.
//   - There is no client and no reply; an error reply becomes the returned
//     error so recovery can log it.
//
// The store should be in loading mode (store.Loader) so passive expiry does
// not drop a key between two records that touched it; EXPIRE-family records
// whose time has passed are kept with their past deadline for recovery to
// drop, instead of being deleted on the spot.
func Replay(srv *server.Server, st store.Store, nowMs int64, args [][]byte) error {
	if len(args) == 0 {
		return fmt.Errorf("command: replay: empty record")
	}
	cmd, ok := table[strings.ToUpper(string(args[0]))]
	if !ok {
		return fmt.Errorf("command: replay: unknown command %q", args[0])
	}
	if !arityOK(cmd.Arity, len(args)) {
		return fmt.Errorf("command: replay: wrong number of arguments for %q", args[0])
	}
	if cmd.Flags&Write == 0 {
		return fmt.Errorf("command: replay: %q is not a write", args[0])
	}
	ctx := &Ctx{Client: &server.Client{}, Store: st, Srv: srv, Now: nowMs, replaying: true}
	reply := cmd.Handler(ctx, args)
	if resp.IsError(reply) {
		return fmt.Errorf("command: replay: %s: %s", args[0], strings.TrimSpace(strings.TrimPrefix(string(reply.AppendTo(nil)), "-")))
	}
	return nil
}
