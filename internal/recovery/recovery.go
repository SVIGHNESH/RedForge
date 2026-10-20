package recovery

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/aof"
	"github.com/SVIGHNESH/RedForge/internal/command"
	"github.com/SVIGHNESH/RedForge/internal/server"
	"github.com/SVIGHNESH/RedForge/internal/snapshot"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

// Snapshot load outcomes for INFO snapshot_last_load_status.
const (
	SnapshotNone = "none" // no dump.snap in the directory
	SnapshotOK   = "ok"   // loaded and verified
	SnapshotErr  = "err"  // present but failed verification; ignored
)

// Result is what one startup recovered.
type Result struct {
	// OrphansRemoved counts dump.snap.tmp-* files left by a crashed save.
	OrphansRemoved int
	// Snapshot is SnapshotNone, SnapshotOK or SnapshotErr; SnapshotKeys
	// and SnapshotSeq describe a loaded snapshot.
	Snapshot     string
	SnapshotKeys int
	SnapshotSeq  uint64
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

// Run is the startup sequence of MASTER-PLAN Section 4.9, before the log is
// opened for append and before the server listens:
//
//	delete dump.snap.tmp-*
//	if dump.snap exists and verifies: load it, seq = max_seq; else warn
//	replay appendonly.aof records with seq > max_seq; stop and truncate at
//	  the first bad record
//	drop every key whose expire_at <= now; count them as expired_keys
//	seq = max(seq, last replayed seq)
//
// The store must be empty and come from store.New (it needs store.Loader).
func Run(dir string, st store.Store, srv *server.Server, clock store.Clock) (Result, error) {
	loader, ok := st.(store.Loader)
	if !ok {
		return Result{}, fmt.Errorf("recovery: store does not support loading")
	}
	res := Result{Snapshot: SnapshotNone}

	n, err := snapshot.CleanOrphans(dir)
	if err != nil {
		return res, fmt.Errorf("recovery: %w", err)
	}
	res.OrphansRemoved = n
	if n > 0 {
		log.Printf("recovery: removed %d orphaned snapshot temp file(s) from a save that did not finish", n)
	}

	loader.SetLoading(true)
	defer loader.SetLoading(false)

	start := time.Now()
	if err := loadSnapshot(dir, st, clock, &res); err != nil {
		return res, err
	}
	if res.Snapshot == SnapshotOK {
		log.Printf("recovery: loaded %d keys from %s (max_seq %d) in %s", res.SnapshotKeys, snapshot.FileName, res.SnapshotSeq, time.Since(start).Round(time.Microsecond))
	}

	start = time.Now()
	now := clock.NowMs()
	last, replayed, err := aof.Replay(filepath.Join(dir, aof.FileName), res.SnapshotSeq, func(seq uint64, args [][]byte) {
		if err := command.Replay(srv, st, now, args); err != nil {
			res.ReplayErrors++
			log.Printf("recovery: record seq %d: %v", seq, err)
		}
	})
	res.Log = replayed
	if err != nil {
		return res, fmt.Errorf("recovery: %w", err)
	}
	log.Printf("recovery: replayed %d log records after seq %d (skipped %d already in the snapshot, last seq %d) in %s",
		replayed.Applied, res.SnapshotSeq, replayed.Skipped, last, time.Since(start).Round(time.Microsecond))

	loader.SetLoading(false)
	res.Expired = loader.DropExpired()
	if res.Expired > 0 {
		log.Printf("recovery: dropped %d expired key(s) whose deadline passed while the server was down", res.Expired)
	}

	res.LastSeq = max(res.SnapshotSeq, last)
	srv.SetSeq(res.LastSeq)
	return res, nil
}

// loadSnapshot loads dump.snap into a scratch store, and copies the entries
// into st only once the whole file, CRC included, has verified. A corrupt
// snapshot therefore never touches the live keyspace: it is logged, reported
// as SnapshotErr, and recovery continues from the log alone, because a bad
// snapshot must not take the server down (MASTER-PLAN 4.8).
func loadSnapshot(dir string, st store.Store, clock store.Clock, res *Result) error {
	f, err := os.Open(filepath.Join(dir, snapshot.FileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("recovery: open snapshot: %w", err)
	}
	defer f.Close()

	scratch := store.New(clock)
	scratch.(store.Loader).SetLoading(true)
	maxSeq, err := snapshot.ReadInto(f, scratch.Put)
	if err != nil {
		res.Snapshot = SnapshotErr
		log.Printf("recovery: WARNING: ignoring %s: %v; recovering from the log alone", snapshot.FileName, err)
		return nil
	}
	scratch.ForEach(func(k string, e *store.Entry) bool {
		st.Put(k, e)
		return true
	})
	res.Snapshot = SnapshotOK
	res.SnapshotKeys = scratch.Len()
	res.SnapshotSeq = maxSeq
	return nil
}

// Info reports the startup outcome for the INFO Persistence section.
func (r Result) Info() []server.InfoField {
	fields := r.Log.Info()
	return append(fields,
		server.InfoField{Key: "snapshot_last_load_status", Value: r.Snapshot},
		server.InfoField{Key: "snapshot_last_load_keys", Value: strconv.Itoa(r.SnapshotKeys)},
		server.InfoField{Key: "snapshot_last_load_seq", Value: strconv.FormatUint(r.SnapshotSeq, 10)},
	)
}
