package store

import (
	"testing"
	"time"
)

func expiredKeys(s Store) int64 { return s.(Counters).ExpiredKeys() }

func TestSetExpireAtAndTTLms(t *testing.T) {
	s, clock := newTestStore()
	now := clock.NowMs()
	if s.SetExpireAt("missing", now+1000) {
		t.Fatal("SetExpireAt on a missing key = true")
	}
	if s.ExpiringLen() != 0 {
		t.Fatal("SetExpireAt on a missing key touched the index")
	}
	putString(s, "k", "v", 0)
	if ms, st := s.TTLms("k"); st != NoExpire || ms != 0 {
		t.Fatalf("TTLms before expiry = %d,%v, want 0,NoExpire", ms, st)
	}
	if !s.SetExpireAt("k", now+10_000) {
		t.Fatal("SetExpireAt on a live key = false")
	}
	if ms, st := s.TTLms("k"); st != HasExpire || ms != 10_000 {
		t.Fatalf("TTLms = %d,%v, want 10000,HasExpire", ms, st)
	}
	if s.ExpiringLen() != 1 {
		t.Fatalf("ExpiringLen = %d, want 1", s.ExpiringLen())
	}
	if s.SetExpireAt("k", 0) || s.SetExpireAt("k", -5) {
		t.Fatal("SetExpireAt accepted a non-positive timestamp")
	}
	if ms, _ := s.TTLms("missing"); ms != 0 {
		t.Fatalf("TTLms of a missing key = %d, want 0", ms)
	}
	if _, st := s.TTLms("missing"); st != NoKey {
		t.Fatalf("TTLms status of a missing key = %v, want NoKey", st)
	}
}

// The boundary is exact: a key is alive 1 ms before ExpireAt and gone at it,
// on every read path, and the reclaim is counted once.
func TestPassiveExpiryBoundaryOnEveryPath(t *testing.T) {
	paths := map[string]func(s Store) bool{
		"Lookup":  func(s Store) bool { _, ok := s.Lookup("k"); return ok },
		"Mutable": func(s Store) bool { _, ok := s.Mutable("k"); return ok },
		"Exists":  func(s Store) bool { return s.Exists("k") },
		"Keys":    func(s Store) bool { return len(s.Keys("*")) == 1 },
		"TTLms":   func(s Store) bool { _, st := s.TTLms("k"); return st != NoKey },
	}
	for name, alive := range paths {
		s, clock := newTestStore()
		putString(s, "k", "v", 0)
		s.SetExpireAt("k", clock.NowMs()+1000)
		clock.Advance(999 * time.Millisecond)
		if !alive(s) {
			t.Errorf("%s: key gone 1 ms before its expiry", name)
			continue
		}
		clock.Advance(time.Millisecond)
		if alive(s) {
			t.Errorf("%s: key still visible at its expiry instant", name)
		}
		if s.Len() != 0 || s.ExpiringLen() != 0 {
			t.Errorf("%s: expired key not reclaimed: Len=%d ExpiringLen=%d", name, s.Len(), s.ExpiringLen())
		}
		if got := expiredKeys(s); got != 1 {
			t.Errorf("%s: ExpiredKeys = %d, want 1", name, got)
		}
	}
}

func TestPersistRemovesOnlyAnExistingTTL(t *testing.T) {
	s, clock := newTestStore()
	putString(s, "k", "v", 0)
	if s.Persist("k") {
		t.Fatal("Persist on a key without TTL = true")
	}
	if s.Persist("missing") {
		t.Fatal("Persist on a missing key = true")
	}
	s.SetExpireAt("k", clock.NowMs()+5000)
	if !s.Persist("k") {
		t.Fatal("Persist on a key with TTL = false")
	}
	if _, st := s.TTLms("k"); st != NoExpire {
		t.Fatalf("TTLms after Persist status = %v, want NoExpire", st)
	}
	if s.ExpiringLen() != 0 {
		t.Fatalf("ExpiringLen after Persist = %d, want 0", s.ExpiringLen())
	}
	clock.Advance(time.Hour)
	if !s.Exists("k") {
		t.Fatal("a persisted key expired anyway")
	}
}

// Changing an expiry must not write through a shared *Entry: a snapshot clone
// (T6.02) holds the same pointers and must keep the old TTL.
func TestExpiryChangesNeverMutateASharedEntry(t *testing.T) {
	s, clock := newTestStore()
	putString(s, "k", "v", 0)
	before, _ := s.Lookup("k")

	s.SetExpireAt("k", clock.NowMs()+1000)
	if before.ExpireAt != 0 {
		t.Fatalf("SetExpireAt wrote through the old entry: ExpireAt=%d", before.ExpireAt)
	}
	withTTL, _ := s.Lookup("k")
	s.Persist("k")
	if withTTL.ExpireAt == 0 {
		t.Fatal("Persist wrote through the old entry")
	}
	after, _ := s.Lookup("k")
	if after.Val != before.Val {
		t.Fatal("expiry change replaced the value instead of sharing it")
	}
}

// Put of an entry without expiry clears the index; Delete clears it too.
func TestIndexFollowsPutAndDelete(t *testing.T) {
	s, clock := newTestStore()
	putString(s, "a", "v", 0)
	putString(s, "b", "v", 0)
	s.SetExpireAt("a", clock.NowMs()+1000)
	s.SetExpireAt("b", clock.NowMs()+1000)
	putString(s, "a", "new", 0)
	s.Delete("b")
	if s.ExpiringLen() != 0 {
		t.Fatalf("ExpiringLen = %d, want 0 after Put and Delete", s.ExpiringLen())
	}
	if expiredKeys(s) != 0 {
		t.Fatalf("explicit Put/Delete counted as expiry: %d", expiredKeys(s))
	}
}
