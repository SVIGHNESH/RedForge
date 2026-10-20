package store

import (
	"strconv"
	"testing"
	"time"
)

// fakeSweepTime makes every elapsed-time check inside SweepExpired report
// perCheck more than the last one, so a 1 ms budget always allows the same
// number of sample rounds no matter how loaded the machine is.
func fakeSweepTime(t *testing.T, perCheck time.Duration) {
	t.Helper()
	var calls time.Duration
	prev := sweepElapsed
	sweepElapsed = func(time.Time) time.Duration {
		calls++
		return calls * perCheck
	}
	t.Cleanup(func() { sweepElapsed = prev })
}

// roundsPerBudget is how many sample rounds a 1 ms budget allows when each
// round is charged 200 us.
const roundsPerBudget = 5

// Ten thousand keys expiring at once are all reclaimed by repeated sweeps,
// every call stops within its budget, and nothing is reclaimed early.
func TestSweepReclaimsTenThousandKeysWithinBudget(t *testing.T) {
	s, clock := newTestStore()
	ms := s.(*mapStore)
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
		fakeSweepTime(t, 200*time.Microsecond)
		before := ms.sweepRounds
		total += s.SweepExpired(20, time.Millisecond)
		if rounds := ms.sweepRounds - before; rounds > roundsPerBudget {
			t.Fatalf("sweep call %d took %d rounds, want at most %d within the budget", calls, rounds, roundsPerBudget)
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
	ms := s.(*mapStore)
	const n = 200_000
	for i := 0; i < n; i++ {
		putString(s, "k"+strconv.Itoa(i), "v", clock.NowMs()+1)
	}
	clock.Advance(time.Millisecond)
	fakeSweepTime(t, 200*time.Microsecond)
	got := s.SweepExpired(20, time.Millisecond)
	if ms.sweepRounds != roundsPerBudget {
		t.Fatalf("budgeted sweep took %d rounds, want exactly %d", ms.sweepRounds, roundsPerBudget)
	}
	if got != 20*roundsPerBudget {
		t.Fatalf("one budgeted sweep deleted %d of %d keys, want %d", got, n, 20*roundsPerBudget)
	}
}

// With the real clock the budget still stops a call that has a huge expired
// set to chew through. There is no wall-time assertion: it guards against
// the budget check being removed, not against a busy machine.
func TestSweepBudgetHoldsOnTheRealClock(t *testing.T) {
	s, clock := newTestStore()
	const n = 200_000
	for i := 0; i < n; i++ {
		putString(s, "k"+strconv.Itoa(i), "v", clock.NowMs()+1)
	}
	clock.Advance(time.Millisecond)
	if got := s.SweepExpired(20, time.Millisecond); got == 0 || got == n {
		t.Fatalf("one budgeted sweep deleted %d of %d keys, want some but not all", got, n)
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
