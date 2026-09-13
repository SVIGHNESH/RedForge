package storetest

import (
	"sync/atomic"
	"time"
)

// FakeClock is a deterministic Clock for expiry tests. The zero value reads
// 0 ms; Advance moves it forward. It is safe for concurrent use so the T6.02
// race test can read the snapshot view from a goroutine while the test
// mutates through the store.
type FakeClock struct {
	ms atomic.Int64
}

// NowMs returns the fake time in Unix milliseconds.
func (c *FakeClock) NowMs() int64 { return c.ms.Load() }

// Set jumps the clock to an absolute millisecond timestamp.
func (c *FakeClock) Set(ms int64) { c.ms.Store(ms) }

// Advance moves the clock forward by d. Negative durations move it backwards,
// which tests must avoid except to set up fixtures.
func (c *FakeClock) Advance(d time.Duration) { c.ms.Add(int64(d / time.Millisecond)) }
