package server

import (
	"sync/atomic"
	"testing"
	"time"
)

// A goroutine signalling an eventfd wakes the loop, which runs the
// registered callback on the loop thread once per drained signal batch, and
// a drained descriptor does not fire again.
func TestNotifierWakesTheLoop(t *testing.T) {
	s := New()
	n, err := NewNotifier()
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	var calls atomic.Int64
	// Registered before Listen: applied when the epoll instance exists.
	if err := s.RegisterFd(n.FD(), func() { n.Drain(); calls.Add(1) }); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterFd(n.FD(), func() {}); err == nil {
		t.Fatal("registering the same fd twice succeeded")
	}
	if err := s.Listen("127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Serve() }()
	defer func() { s.Stop(); <-done; s.Close() }()

	go n.Signal()
	waitUntil(t, func() bool { return calls.Load() == 1 })
	time.Sleep(250 * time.Millisecond) // two ticks: a drained fd stays quiet
	if got := calls.Load(); got != 1 {
		t.Fatalf("callback ran %d times for one signal", got)
	}
	n.Signal()
	waitUntil(t, func() bool { return calls.Load() == 2 })
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 2 s")
		}
		time.Sleep(time.Millisecond)
	}
}
