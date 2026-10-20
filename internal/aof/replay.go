package aof

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"

	"github.com/SVIGHNESH/RedForge/internal/server"
)

// ReplayResult describes what Replay found.
type ReplayResult struct {
	// Applied counts records passed to apply, Skipped those at or below
	// afterSeq (already contained in the snapshot).
	Applied int
	Skipped int
	// TruncatedAt is the offset the file was cut back to because the
	// record starting there was torn or corrupt, or -1 when the log was
	// clean. Truncated is 1 in that case and 0 otherwise: replay stops at
	// the first bad record, so it can never discard more than one.
	TruncatedAt int64
	Truncated   int
	// Reason is ErrTorn or ErrCorrupt when the log was truncated.
	Reason error
}

// Clean reports a result for a log with nothing to truncate.
func Clean() ReplayResult { return ReplayResult{TruncatedAt: -1} }

// Replay reads the log at path and calls apply for every record whose seq
// is above afterSeq, in file order. It returns the highest valid seq it saw,
// whether applied or skipped.
//
// The first torn or corrupt record ends replay: the file is truncated to
// where that record starts and synced, the event is logged, and Replay
// returns normally with the offset in the result. That record was never
// acknowledged as durable, so dropping it and anything after it is the
// write-ahead-log policy of Proposal Section 6.4.
//
// A missing file is an empty log. A file shorter than the header whose bytes
// are a prefix of it (a crash while the log was being created) is cut to
// zero so Open writes a fresh header. Any other header mismatch is an error:
// the file is not ours, and truncating it would destroy someone's data.
func Replay(path string, afterSeq uint64, apply func(seq uint64, args [][]byte)) (uint64, ReplayResult, error) {
	res := Clean()
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return 0, res, nil
	}
	if err != nil {
		return 0, res, fmt.Errorf("aof: open for replay: %w", err)
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return 0, res, fmt.Errorf("aof: stat: %w", err)
	}
	if st.Size() < int64(HeaderSize) {
		head := make([]byte, st.Size())
		if _, err := io.ReadFull(f, head); err != nil {
			return 0, res, fmt.Errorf("aof: read header: %w", err)
		}
		if !bytes.HasPrefix(append([]byte(magic), Version), head) {
			return 0, res, fmt.Errorf("%w: %q", ErrBadHeader, head)
		}
		if st.Size() > 0 {
			res = ReplayResult{TruncatedAt: 0, Truncated: 1, Reason: ErrTorn}
			if err := truncate(f, 0); err != nil {
				return 0, res, err
			}
			log.Printf("aof: log ends inside its header; truncated to 0 bytes")
		}
		return 0, res, nil
	}
	if err := ReadHeader(f); err != nil {
		return 0, res, err
	}

	rd := NewReader(f, int64(HeaderSize))
	var last uint64
	for {
		seq, args, err := rd.Next()
		if err == io.EOF {
			return last, res, nil
		}
		if errors.Is(err, ErrTorn) || errors.Is(err, ErrCorrupt) {
			at := rd.LastGoodOffset()
			res.TruncatedAt, res.Truncated, res.Reason = at, 1, err
			if terr := truncate(f, at); terr != nil {
				return last, res, terr
			}
			// err already reads "aof: torn record" or "aof: corrupt record".
			log.Printf("%v at offset %d after seq %d; truncated the log to %d bytes", err, at, last, at)
			return last, res, nil
		}
		if err != nil {
			return last, res, fmt.Errorf("aof: read: %w", err)
		}
		last = seq
		if seq <= afterSeq {
			res.Skipped++
			continue
		}
		apply(seq, args)
		res.Applied++
	}
}

func truncate(f *os.File, size int64) error {
	if err := f.Truncate(size); err != nil {
		return fmt.Errorf("aof: truncate to %d: %w", size, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("aof: sync after truncate: %w", err)
	}
	return nil
}

// Info reports the replay outcome for the INFO Persistence section.
func (r ReplayResult) Info() []server.InfoField {
	return []server.InfoField{
		{Key: "aof_last_truncation_offset", Value: strconv.FormatInt(r.TruncatedAt, 10)},
		{Key: "aof_truncated_records", Value: strconv.Itoa(r.Truncated)},
	}
}
