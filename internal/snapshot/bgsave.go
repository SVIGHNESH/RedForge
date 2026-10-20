package snapshot

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/server"
	"github.com/SVIGHNESH/RedForge/internal/store"
)

// File names from MASTER-PLAN Section 3.6.
const (
	// FileName is the live snapshot inside the data directory.
	FileName = "dump.snap"
	// tmpPrefix starts every in-progress snapshot; the pid follows.
	tmpPrefix = FileName + ".tmp-"
)

// Saver runs BGSAVE. It implements server.BackgroundSaver.
//
// Start runs on the loop thread and launches the writer goroutine, which
// serialises the view to dump.snap.tmp-<pid>, fsyncs it, renames it over
// dump.snap, fsyncs the directory, sends its result on a channel and
// signals an eventfd. The loop wakes on the eventfd, takes the result from
// the channel, releases the view and updates the INFO fields. The goroutine
// never touches the live keyspace or any Saver field other than the channel.
type Saver struct {
	st    store.Store
	dir   string
	delay time.Duration
	nowMs func() int64
	note  *server.Notifier
	done  chan error

	// Loop-thread state.
	running  bool
	view     store.SnapshotView
	boundary uint64
	started  time.Time
	cloneMs  int64

	lastStatus   string
	lastSeq      uint64
	lastTime     int64 // Unix seconds of the last successful save
	lastDuration time.Duration
	lastErr      error
	saves        int64

	// onDone, when set, runs on the loop after each save; tests and the
	// log compaction stretch item (S3) hook in here.
	onDone func(boundary uint64, err error)
}

var _ server.BackgroundSaver = (*Saver)(nil)

// NewSaver creates a saver writing st's snapshots to dir and registers its eventfd with srv, so
// the completion callback runs on the loop thread. delay is the
// --debug-bgsave-delay-ms test hook: the writer sleeps that long before the
// rename, so a crash test can kill the process mid-save deterministically.
// It must never be set in the evaluation.
func NewSaver(srv *server.Server, st store.Store, dir string, delay time.Duration, nowMs func() int64) (*Saver, error) {
	note, err := server.NewNotifier()
	if err != nil {
		return nil, err
	}
	sv := &Saver{st: st, dir: dir, delay: delay, nowMs: nowMs, note: note, done: make(chan error, 1), lastStatus: "ok"}
	if err := srv.RegisterFd(note.FD(), sv.complete); err != nil {
		note.Close()
		return nil, err
	}
	return sv, nil
}

// InProgress implements server.BackgroundSaver.
func (sv *Saver) InProgress() bool { return sv.running }

// Start implements server.BackgroundSaver. The view is released here if the
// save cannot start, and otherwise when it completes.
func (sv *Saver) Start(boundary uint64, view store.SnapshotView) error {
	if sv.running {
		view.Release()
		return errors.New("Background save already in progress")
	}
	sv.running = true
	sv.view = view
	sv.boundary = boundary
	sv.started = time.Now()
	sv.cloneMs = 0
	if ct, ok := sv.st.(store.CloneTimer); ok {
		sv.cloneMs = ct.LastCloneDuration().Milliseconds()
	}
	go sv.write(boundary, view)
	return nil
}

// write is the snapshot goroutine. It reads only the view, which nothing
// mutates (copy-on-write in Store.Mutable), and the filesystem.
func (sv *Saver) write(boundary uint64, view store.SnapshotView) {
	err := WriteFile(sv.dir, boundary, view, sv.delay)
	sv.done <- err
	if serr := sv.note.Signal(); serr != nil {
		log.Printf("snapshot: eventfd signal failed: %v", serr)
	}
}

// complete runs on the loop thread when the eventfd is readable.
func (sv *Saver) complete() {
	sv.note.Drain()
	var err error
	select {
	case err = <-sv.done:
	default:
		return // spurious wakeup: the result is not there yet
	}
	sv.view.Release()
	sv.view = nil
	sv.running = false
	sv.lastDuration = time.Since(sv.started)
	sv.lastErr = err
	if err != nil {
		sv.lastStatus = "err"
		log.Printf("snapshot: BGSAVE failed: %v", err)
	} else {
		sv.lastStatus = "ok"
		sv.lastSeq = sv.boundary
		sv.lastTime = sv.nowMs() / 1000
		sv.saves++
		log.Printf("snapshot: BGSAVE done at seq %d in %s", sv.boundary, sv.lastDuration.Round(time.Microsecond))
	}
	if sv.onDone != nil {
		sv.onDone(sv.boundary, err)
	}
}

// Loaded tells the saver about the snapshot startup recovery loaded, so INFO
// last_bgsave_seq and last_bgsave_time describe the dump.snap on disk after
// a restart instead of reading 0, as Redis reports its last save time from
// the file it loaded.
func (sv *Saver) Loaded(maxSeq uint64) {
	sv.lastSeq = maxSeq
	sv.lastStatus = "ok"
	if st, err := os.Stat(filepath.Join(sv.dir, FileName)); err == nil {
		sv.lastTime = st.ModTime().Unix()
	}
}

// OnDone registers fn to run on the loop thread after every save.
func (sv *Saver) OnDone(fn func(boundary uint64, err error)) { sv.onDone = fn }

// Info reports the BGSAVE fields for the INFO Persistence section.
func (sv *Saver) Info() []server.InfoField {
	inProgress := "0"
	if sv.running {
		inProgress = "1"
	}
	return []server.InfoField{
		{Key: "bgsave_in_progress", Value: inProgress},
		{Key: "last_bgsave_status", Value: sv.lastStatus},
		{Key: "last_bgsave_seq", Value: strconv.FormatUint(sv.lastSeq, 10)},
		{Key: "last_bgsave_clone_ms", Value: strconv.FormatInt(sv.cloneMs, 10)},
		{Key: "last_bgsave_time", Value: strconv.FormatInt(sv.lastTime, 10)},
		{Key: "last_bgsave_duration_ms", Value: strconv.FormatInt(sv.lastDuration.Milliseconds(), 10)},
	}
}

// Close releases the eventfd. A save still running is abandoned; its
// temporary file is an orphan that the next startup deletes.
func (sv *Saver) Close() error { return sv.note.Close() }

// WriteFile writes a complete snapshot atomically: temp file, fsync, the
// optional test delay, rename over dump.snap, then fsync the directory so the
// rename itself is durable. A crash at any point before the rename leaves the
// previous dump.snap intact. On error the temp file is removed.
func WriteFile(dir string, maxSeq uint64, src Source, delay time.Duration) (err error) {
	tmp := filepath.Join(dir, tmpPrefix+strconv.Itoa(os.Getpid()))
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("snapshot: create temp file: %w", err)
	}
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()
	bw := bufio.NewWriterSize(f, 1<<20)
	if err = Write(bw, maxSeq, src); err != nil {
		return err
	}
	if err = bw.Flush(); err != nil {
		return fmt.Errorf("snapshot: write: %w", err)
	}
	if err = f.Sync(); err != nil {
		return fmt.Errorf("snapshot: fsync: %w", err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("snapshot: close: %w", err)
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	if err = os.Rename(tmp, filepath.Join(dir, FileName)); err != nil {
		return fmt.Errorf("snapshot: rename: %w", err)
	}
	return syncDir(dir)
}

// CleanOrphans deletes every dump.snap.tmp-* left by a save that crashed
// before its rename, and returns how many it removed.
func CleanOrphans(dir string) (int, error) {
	matches, err := filepath.Glob(filepath.Join(dir, tmpPrefix+"*"))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, m := range matches {
		if err := os.Remove(m); err != nil && !errors.Is(err, os.ErrNotExist) {
			return n, fmt.Errorf("snapshot: remove orphan: %w", err)
		}
		n++
	}
	return n, nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("snapshot: open dir: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("snapshot: fsync dir: %w", err)
	}
	return nil
}
