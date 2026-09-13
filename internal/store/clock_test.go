package store

import (
	"testing"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/storetest"
)

func TestRealClockIsCloseToWallClock(t *testing.T) {
	var c RealClock
	before := time.Now().UnixMilli()
	got := c.NowMs()
	after := time.Now().UnixMilli()
	if got < before || got > after {
		t.Errorf("RealClock.NowMs() = %d, want within [%d, %d]", got, before, after)
	}
}

func TestFakeClockAdvance(t *testing.T) {
	c := &storetest.FakeClock{}
	c.Set(5000)
	if c.NowMs() != 5000 {
		t.Fatalf("NowMs() = %d, want 5000", c.NowMs())
	}
	c.Advance(1500 * time.Millisecond)
	if c.NowMs() != 6500 {
		t.Errorf("after Advance(1.5s) NowMs() = %d, want 6500", c.NowMs())
	}
}

func TestNilClockMeansWallClock(t *testing.T) {
	s := New(nil)
	if s == nil {
		t.Fatal("New(nil) = nil, want a usable store")
	}
	s.Put("k", &Entry{Type: TString, Val: &StringVal{B: []byte("v")}})
	if !s.Exists("k") {
		t.Error("store with nil clock lost a key without expiry")
	}
}
