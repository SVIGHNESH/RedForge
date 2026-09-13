package store

import "time"

// Clock abstracts time so expiry tests are deterministic. Production uses
// RealClock (wall clock, as the synopsis states); tests use a fake clock from
// internal/storetest. All expiry comparisons go through NowMs, never through
// time.Now directly (MASTER-PLAN Section 3.4).
type Clock interface {
	NowMs() int64
}

// RealClock reads the wall clock.
type RealClock struct{}

// NowMs returns the current Unix time in milliseconds.
func (RealClock) NowMs() int64 { return time.Now().UnixMilli() }
