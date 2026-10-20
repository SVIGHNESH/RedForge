package store

import (
	"strconv"
	"testing"
	"time"
)

// Ten thousand keys expiring at once are all reclaimed by repeated sweeps,
// with every call bounded in wall time and nothing reclaimed early.
func TestSweepReclaimsTenThousandKeysWithinBudget(t *testing.T) {
	s, clock := newTestStore()
	const n = 10_000
	for i := 0; i < n; i++ {
		putString(s, "k"+strconv.Itoa(i), "v", clock.NowMs()+1)
	}
	putString(s, "keep", "v", 0)
	if got := s.SweepExpired(20, time.Millisecond); got != 0 {
		t.Fatalf("sweep before expiry deleted %d keys", got)
	}

	clock.Advance(time.Millisecond)
	total, calls := 0, 0
	for s.ExpiringLen() > 0 {
		start := time.Now()
		total += s.SweepExpired(20, time.Millisecond)
		if d := time.Since(start); d > 5*time.Millisecond {
			t.Fatalf("sweep call %d took %v, want under 5 ms", calls, d)
		}
		calls++
		if calls > n {
			t.Fatal("sweep made no progress")
		}
	}
	if total != n || s.Len() != 1 {
		t.Fatalf("swept %d keys, Len=%d; want %d swept and only 'keep' left", total, s.Len(), n)
	}
	c := s.(Counters)
	if c.ExpiredKeys() != n || c.SweepDeleted() != n {
		t.Fatalf("ExpiredKeys=%d SweepDeleted=%d, want %d each", c.ExpiredKeys(), c.SweepDeleted(), n)
	}
	t.Logf("%d keys reclaimed in %d sweep calls", n, calls)
}

// The 1 ms budget is what keeps the loop responsive: with everything expired
// the sweep keeps resampling, but must stop at the budget rather than
// draining all keys in one call.
func TestSweepStopsAtTheBudget(t *testing.T) {
	s, clock := newTestStore()
	const n = 200_000
	for i := 0; i < n; i++ {
		putString(s, "k"+strconv.Itoa(i), "v", clock.NowMs()+1)
	}
	clock.Advance(time.Millisecond)
	start := time.Now()
	got := s.SweepExpired(20, time.Millisecond)
	elapsed := time.Since(start)
	if got == 0 || got == n {
		t.Fatalf("one budgeted sweep deleted %d of %d keys, want some but not all", got, n)
	}
	if elapsed > 5*time.Millisecond {
		t.Fatalf("budgeted sweep ran %v, want close to 1 ms", elapsed)
	}
}

// With nothing expired the sweep takes exactly one sample and stops.
func TestSweepWithNothingExpiredSamplesOnce(t *testing.T) {
	s, clock := newTestStore()
	for i := 0; i < 1000; i++ {
		putString(s, "k"+strconv.Itoa(i), "v", clock.NowMs()+60_000)
	}
	ms := s.(*mapStore)
	if got := s.SweepExpired(20, time.Millisecond); got != 0 {
		t.Fatalf("sweep deleted %d live keys", got)
	}
	if ms.sweepRounds != 1 {
		t.Fatalf("sweep took %d samples, want 1", ms.sweepRounds)
	}
	if s.Len() != 1000 {
		t.Fatalf("Len = %d, want 1000", s.Len())
	}
}

// A sample at or under 25 percent expired ends the sweep; over 25 percent
// triggers another round.
func TestSweepRepeatsOnlyAboveTwentyFivePercent(t *testing.T) {
	s, clock := newTestStore()
	// 5 of 20 expiring keys expired: exactly 25 percent, so one round.
	for i := 0; i < 20; i++ {
		at := clock.NowMs() + 60_000
		if i < 5 {
			at = clock.NowMs() + 1
		}
		putString(s, "k"+strconv.Itoa(i), "v", at)
	}
	clock.Advance(time.Millisecond)
	ms := s.(*mapStore)
	if got := s.SweepExpired(20, time.Second); got != 5 {
		t.Fatalf("sweep deleted %d, want 5", got)
	}
	if ms.sweepRounds != 1 {
		t.Fatalf("25 percent expired took %d rounds, want 1", ms.sweepRounds)
	}
}

func TestSweepIgnoresKeysWithoutExpiryAndBadSampleSize(t *testing.T) {
	s, _ := newTestStore()
	putString(s, "a", "v", 0)
	if got := s.SweepExpired(20, time.Millisecond); got != 0 {
		t.Fatalf("sweep on a store with no expiring keys deleted %d", got)
	}
	if s.(*mapStore).sweepRounds != 0 {
		t.Fatal("sweep sampled an empty expiring index")
	}
	if got := s.SweepExpired(0, time.Millisecond); got != 0 {
		t.Fatalf("SweepExpired(0) = %d, want 0", got)
	}
}
