package server

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// startTickServer is a bare loop with one ticker counting its calls.
func startTickServer(t *testing.T) (*atomic.Int64, string) {
	t.Helper()
	s := New()
	var ticks atomic.Int64
	s.OnTick(func() { ticks.Add(1) })
	if err := s.Listen("127.0.0.1", 0); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	port, err := s.Port()
	if err != nil {
		t.Fatalf("Port: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Serve() }()
	t.Cleanup(func() {
		s.Stop()
		<-done
		s.Close()
	})
	return &ticks, fmt.Sprintf("127.0.0.1:%d", port)
}

// An idle loop ticks about every 100 ms.
func TestIdleLoopTicksEveryHundredMs(t *testing.T) {
	ticks, _ := startTickServer(t)
	time.Sleep(1050 * time.Millisecond)
	if got := ticks.Load(); got < 7 || got > 11 {
		t.Fatalf("ticks in 1.05 s = %d, want about 10", got)
	}
}

// A loop that is woken by I/O far more often than every 100 ms must still
// tick on schedule, and must not tick on every wakeup.
func TestBusyLoopStillTicksOnSchedule(t *testing.T) {
	ticks, addr := startTickServer(t)
	conn := dial(t, addr)
	deadline := time.Now().Add(1050 * time.Millisecond)
	sent := 0
	for time.Now().Before(deadline) {
		if _, err := conn.Write([]byte("PING\r\n")); err != nil {
			t.Fatalf("write: %v", err)
		}
		readN(t, conn, len("+OK\r\n"))
		sent++
	}
	if sent < 100 {
		t.Fatalf("only %d round trips; the test did not keep the loop busy", sent)
	}
	if got := ticks.Load(); got < 7 || got > 11 {
		t.Fatalf("ticks in 1.05 s under %d round trips = %d, want about 10", sent, got)
	}
}

func TestWaitMsRoundsUpAndNeverGoesNegative(t *testing.T) {
	if got := waitMs(time.Now().Add(-time.Second)); got != 0 {
		t.Errorf("waitMs(past) = %d, want 0", got)
	}
	if got := waitMs(time.Now().Add(50 * time.Millisecond)); got < 49 || got > 50 {
		t.Errorf("waitMs(+50ms) = %d, want 49 or 50", got)
	}
	if got := waitMs(time.Now().Add(100 * time.Microsecond)); got != 1 {
		t.Errorf("waitMs(+100us) = %d, want 1 so the loop does not spin", got)
	}
}
