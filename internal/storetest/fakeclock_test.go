package storetest

import (
	"testing"
	"time"
)

func TestFakeClockSetAndAdvance(t *testing.T) {
	var c FakeClock
	if c.NowMs() != 0 {
		t.Fatalf("zero FakeClock = %d, want 0", c.NowMs())
	}
	c.Set(1000)
	c.Advance(250 * time.Millisecond)
	if c.NowMs() != 1250 {
		t.Errorf("NowMs() = %d, want 1250", c.NowMs())
	}
}
