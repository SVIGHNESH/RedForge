package recovery

import (
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/aof"
	"github.com/SVIGHNESH/RedForge/internal/command"
	"github.com/SVIGHNESH/RedForge/internal/server"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

// Result is what one startup recovered.
type Result struct {
	// Log is the replay outcome, including any truncation.
	Log aof.ReplayResult
	// LastSeq is where the sequence counter resumed.
	LastSeq uint64
	// Expired counts keys dropped because their absolute expiry passed
	// while the server was down.
	Expired int
	// ReplayErrors counts records that replayed with an error reply. A
	// healthy log has none; each one is logged.
	ReplayErrors int
}

// Run rebuilds st from the log in dir and resumes srv's sequence counter:
//
//	replay appendonly.aof; stop and truncate at the first bad record
//	drop every key whose expire_at <= now; count them as expired_keys
//	seq = last replayed seq
//
// The store must come from store.New (it needs store.Loader). Run is called
// before the log is opened for append and before the server listens, so
// nothing else touches the store or the file.
func Run(dir string, st store.Store, srv *server.Server, clock store.Clock) (Result, error) {
	loader, ok := st.(store.Loader)
	if !ok {
		return Result{}, fmt.Errorf("recovery: store does not support loading")
	}
	var res Result

	start := time.Now()
	now := clock.NowMs()
	loader.SetLoading(true)
	last, replayed, err := aof.Replay(filepath.Join(dir, aof.FileName), 0, func(seq uint64, args [][]byte) {
		if err := command.Replay(srv, st, now, args); err != nil {
			res.ReplayErrors++
			log.Printf("recovery: record seq %d: %v", seq, err)
		}
	})
	loader.SetLoading(false)
	res.Log = replayed
	if err != nil {
		return res, fmt.Errorf("recovery: %w", err)
	}
	log.Printf("recovery: replayed %d log records (last seq %d) in %s", replayed.Applied, last, time.Since(start).Round(time.Microsecond))

	res.Expired = loader.DropExpired()
	if res.Expired > 0 {
		log.Printf("recovery: dropped %d expired key(s) whose deadline passed while the server was down", res.Expired)
	}

	res.LastSeq = last
	srv.SetSeq(last)
	return res, nil
}
