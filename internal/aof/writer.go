package aof

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/SVIGHNESH/RedForge/internal/server"
)

// FileName is the log's name inside the data directory (MASTER-PLAN 3.6).
const FileName = "appendonly.aof"

// Policy is the --appendfsync setting, fixed at startup.
type Policy uint8

const (
	// EverySec writes on the loop and lets the fsync goroutine sync once a
	// second. It is the default.
	EverySec Policy = iota
	// Always syncs on the loop thread before the reply is queued.
	Always
	// No only writes and lets the kernel decide when to flush.
	No
)

// ParsePolicy reads an --appendfsync value.
func ParsePolicy(s string) (Policy, error) {
	switch s {
	case "everysec":
		return EverySec, nil
	case "always":
		return Always, nil
	case "no":
		return No, nil
	}
	return 0, fmt.Errorf("aof: unknown appendfsync policy %q (want always, everysec or no)", s)
}

func (p Policy) String() string {
	switch p {
	case Always:
		return "always"
	case No:
		return "no"
	}
	return "everysec"
}

// syncFile is the part of *os.File the writer uses, so tests can count Sync
// calls without touching a disk.
type syncFile interface {
	io.Writer
	Sync() error
	Close() error
}

// Writer appends propagated writes to the log. It implements
// server.Appender. Append runs on the loop thread; under everysec the only
// other goroutine is the fsync goroutine, which calls Sync on the same file.
// Fsync on a descriptor concurrently with Write on it is safe, so no lock
// is needed (MASTER-PLAN 3.2).
type Writer struct {
	f      syncFile
	policy Policy
	buf    []byte

	// lastWriteErr is set on the loop thread when a Write fails and cleared
	// by the next successful one.
	lastWriteErr error
	// nextSyncMs is when the loop next asks for an everysec fsync.
	nextSyncMs int64

	syncReq chan struct{}
	done    sync.WaitGroup
	closed  bool

	// Written by the fsync goroutine (or the loop under always), read by
	// INFO on the loop thread.
	lastFsyncMs atomic.Int64
	fsyncFailed atomic.Bool
	fsyncs      atomic.Int64
	nowMs       func() int64
}

var _ server.Appender = (*Writer)(nil)

// Open opens <dir>/appendonly.aof for appending, creating the directory and
// the file with its header when absent. Under everysec it starts the fsync
// goroutine. nowMs supplies timestamps for the fsync schedule and INFO.
func Open(dir string, policy Policy, nowMs func() int64) (*Writer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("aof: create dir: %w", err)
	}
	path := filepath.Join(dir, FileName)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("aof: open: %w", err)
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("aof: stat: %w", err)
	}
	if st.Size() == 0 {
		if err := WriteHeader(f); err != nil {
			f.Close()
			return nil, fmt.Errorf("aof: write header: %w", err)
		}
		if err := f.Sync(); err != nil {
			f.Close()
			return nil, fmt.Errorf("aof: sync header: %w", err)
		}
		if err := syncDir(dir); err != nil {
			f.Close()
			return nil, err
		}
	}
	return newWriter(f, policy, nowMs), nil
}

func newWriter(f syncFile, policy Policy, nowMs func() int64) *Writer {
	w := &Writer{f: f, policy: policy, nowMs: nowMs}
	if policy == EverySec {
		// A buffer of one lets the loop's request never block: a request
		// that arrives while a sync is running is coalesced into the next.
		w.syncReq = make(chan struct{}, 1)
		w.done.Add(1)
		go w.fsyncLoop()
	}
	return w
}

// fsyncLoop is the everysec fsync goroutine. It reads nothing but the file
// descriptor and touches no keyspace state.
func (w *Writer) fsyncLoop() {
	defer w.done.Done()
	for range w.syncReq {
		w.sync()
	}
}

func (w *Writer) sync() error {
	err := w.f.Sync()
	w.fsyncs.Add(1)
	if err != nil {
		w.fsyncFailed.Store(true)
		log.Printf("aof: fsync failed: %v", err)
		return err
	}
	w.fsyncFailed.Store(false)
	w.lastFsyncMs.Store(w.nowMs())
	return nil
}

// Append encodes and writes one record. Under always it also syncs before
// returning, so the dispatcher queues the reply only after the record is
// durable. A failed write is logged and reported in INFO; the server keeps
// serving, matching the proposal's durability window.
func (w *Writer) Append(seq uint64, args [][]byte) error {
	w.buf = Encode(w.buf[:0], seq, args)
	if _, err := w.f.Write(w.buf); err != nil {
		w.lastWriteErr = err
		return fmt.Errorf("aof: write seq %d: %w", seq, err)
	}
	w.lastWriteErr = nil
	if w.policy == Always {
		if err := w.sync(); err != nil {
			return fmt.Errorf("aof: fsync seq %d: %w", seq, err)
		}
	}
	return nil
}

// Tick is called from the loop tick. Under everysec it asks the fsync
// goroutine for a sync once a second without ever blocking the loop.
func (w *Writer) Tick() {
	if w.policy != EverySec || w.closed {
		return
	}
	now := w.nowMs()
	if now < w.nextSyncMs {
		return
	}
	w.nextSyncMs = now + 1000
	select {
	case w.syncReq <- struct{}{}:
	default:
	}
}

// Close stops the fsync goroutine, syncs once more and closes the file.
func (w *Writer) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if w.syncReq != nil {
		close(w.syncReq)
		w.done.Wait()
	}
	serr := w.sync()
	cerr := w.f.Close()
	if serr != nil {
		return serr
	}
	return cerr
}

// Info reports the log's fields for the INFO Persistence section.
func (w *Writer) Info() []server.InfoField {
	writeStatus := "ok"
	if w.lastWriteErr != nil {
		writeStatus = "err"
	}
	fsyncStatus := "ok"
	if w.fsyncFailed.Load() {
		fsyncStatus = "err"
	}
	return []server.InfoField{
		{Key: "aof_enabled", Value: "1"},
		{Key: "aof_fsync_policy", Value: w.policy.String()},
		{Key: "aof_last_write_status", Value: writeStatus},
		{Key: "aof_last_fsync_status", Value: fsyncStatus},
		{Key: "aof_last_fsync_ms", Value: strconv.FormatInt(w.lastFsyncMs.Load(), 10)},
	}
}

// syncDir fsyncs a directory so a newly created or renamed file in it is
// durable.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("aof: open dir: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("aof: sync dir: %w", err)
	}
	return nil
}
