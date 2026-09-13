package store

import (
	"testing"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/storetest"
)

func newTestStore() (Store, *storetest.FakeClock) {
	clock := &storetest.FakeClock{}
	clock.Set(1_000_000)
	return New(clock), clock
}

func putString(s Store, key, val string, expireAt int64) {
	s.Put(key, &Entry{Type: TString, Val: &StringVal{B: []byte(val)}, ExpireAt: expireAt})
}

func TestPutLookupDeleteExistsLen(t *testing.T) {
	s, _ := newTestStore()
	if s.Len() != 0 {
		t.Fatalf("Len = %d, want 0", s.Len())
	}
	putString(s, "a", "1", 0)
	putString(s, "b", "2", 0)

	e, ok := s.Lookup("a")
	if !ok || e.Type != TString || string(e.Val.(*StringVal).B) != "1" {
		t.Fatalf("Lookup(a) = %+v, %v; want string 1", e, ok)
	}
	if !s.Exists("b") {
		t.Error("Exists(b) = false, want true")
	}
	if s.Exists("missing") {
		t.Error("Exists(missing) = true, want false")
	}
	if s.Len() != 2 {
		t.Errorf("Len = %d, want 2", s.Len())
	}
	if !s.Delete("a") {
		t.Error("Delete(a) = false, want true")
	}
	if s.Delete("a") {
		t.Error("second Delete(a) = true, want false")
	}
	if _, ok := s.Lookup("a"); ok {
		t.Error("Lookup after Delete = found, want absent")
	}
	if s.Len() != 1 {
		t.Errorf("Len = %d, want 1", s.Len())
	}
}

func TestMutableMatchesLookupForNow(t *testing.T) {
	s, _ := newTestStore()
	putString(s, "k", "v", 0)
	l, lok := s.Lookup("k")
	m, mok := s.Mutable("k")
	if !lok || !mok || l != m {
		t.Errorf("Mutable must equal Lookup for now: %v/%v vs %v/%v", m, mok, l, lok)
	}
	if _, ok := s.Mutable("missing"); ok {
		t.Error("Mutable(missing) = found, want absent")
	}
}

// The acceptance invariant: a Lookup on a key whose ExpireAt is in the past
// returns absent and the key is gone.
func TestPassiveExpiryDeletesOnLookup(t *testing.T) {
	s, clock := newTestStore()
	now := clock.NowMs()
	putString(s, "past", "v", now-1)
	putString(s, "now", "v", now)
	putString(s, "future", "v", now+1000)

	for _, key := range []string{"past", "now"} {
		if _, ok := s.Lookup(key); ok {
			t.Errorf("Lookup(%s) = found, want absent (expired)", key)
		}
		if s.Exists(key) {
			t.Errorf("Exists(%s) = true after expiry, want false", key)
		}
	}
	if _, ok := s.Lookup("future"); !ok {
		t.Error("Lookup(future) = absent, want present")
	}
	if s.Len() != 1 {
		t.Errorf("Len = %d, want 1 after passive reclaim", s.Len())
	}
	// A second lookup must still report absent: the first one deleted the key.
	if _, ok := s.Lookup("past"); ok {
		t.Error("second Lookup(past) = found, want absent")
	}
}

func TestPutReplacesTypeAndClearsExpiryIndex(t *testing.T) {
	s, clock := newTestStore()
	putString(s, "k", "v", clock.NowMs()+5000)
	if got := s.ExpiringLen(); got != 1 {
		t.Fatalf("ExpiringLen = %d, want 1", got)
	}
	// SET overwrites any type and clears the TTL, as Redis does.
	putString(s, "k", "v2", 0)
	if got := s.ExpiringLen(); got != 0 {
		t.Errorf("ExpiringLen after overwrite = %d, want 0", got)
	}
	e, ok := s.Lookup("k")
	if !ok || string(e.Val.(*StringVal).B) != "v2" || e.ExpireAt != 0 {
		t.Errorf("Lookup after overwrite = %+v, %v; want v2 with no expiry", e, ok)
	}
	clock.Advance(10 * time.Second)
	if _, ok := s.Lookup("k"); !ok {
		t.Error("key with cleared TTL expired, want present")
	}
}

func TestDeleteRemovesExpiryIndex(t *testing.T) {
	s, clock := newTestStore()
	putString(s, "k", "v", clock.NowMs()+5000)
	s.Delete("k")
	if got := s.ExpiringLen(); got != 0 {
		t.Errorf("ExpiringLen after Delete = %d, want 0", got)
	}
}

func TestKeysMatchesGlobAndExcludesExpired(t *testing.T) {
	s, clock := newTestStore()
	putString(s, "user:1", "a", 0)
	putString(s, "user:2", "b", 0)
	putString(s, "other:1", "c", 0)
	putString(s, "gone:1", "d", clock.NowMs()-1)

	got := s.Keys("user:*")
	if len(got) != 2 {
		t.Fatalf("Keys(user:*) = %q, want 2 keys", got)
	}
	set := map[string]bool{}
	for _, k := range got {
		set[k] = true
	}
	if !set["user:1"] || !set["user:2"] {
		t.Errorf("Keys(user:*) = %q, want user:1 and user:2", got)
	}
	if all := s.Keys("*"); len(all) != 3 {
		t.Errorf("Keys(*) = %q, want 3 live keys (expired excluded)", all)
	}
	if q := s.Keys("?ser:1"); len(q) != 1 || q[0] != "user:1" {
		t.Errorf("Keys(?ser:1) = %q, want [user:1] (other:1 is 7 bytes, gone:1 is expired)", q)
	}
	putString(s, "aser:1", "e", 0)
	q := s.Keys("?ser:1")
	if len(q) != 2 {
		t.Fatalf("Keys(?ser:1) = %q, want [user:1 aser:1]", q)
	}
	setQ := map[string]bool{}
	for _, k := range q {
		setQ[k] = true
	}
	if !setQ["user:1"] || !setQ["aser:1"] {
		t.Errorf("Keys(?ser:1) = %q, want user:1 and aser:1", q)
	}
	if none := s.Keys("nomatch:*"); len(none) != 0 {
		t.Errorf("Keys(nomatch:*) = %q, want empty", none)
	}
}

func TestForEachVisitsAllAndStops(t *testing.T) {
	s, _ := newTestStore()
	putString(s, "a", "1", 0)
	putString(s, "b", "2", 0)
	putString(s, "c", "3", 0)

	count := 0
	s.ForEach(func(key string, e *Entry) bool {
		count++
		if e == nil || e.Type != TString {
			t.Errorf("ForEach(%s) entry = %+v, want string entry", key, e)
		}
		return true
	})
	if count != 3 {
		t.Errorf("ForEach visited %d keys, want 3", count)
	}
	count = 0
	s.ForEach(func(key string, e *Entry) bool {
		count++
		return false
	})
	if count != 1 {
		t.Errorf("ForEach with immediate stop visited %d keys, want 1", count)
	}
}

func TestExpiryStubsReturnZeroValues(t *testing.T) {
	s, _ := newTestStore()
	putString(s, "k", "v", 0)
	if s.SetExpireAt("k", 123) {
		t.Error("SetExpireAt stub = true, want false until T3.01")
	}
	if s.Persist("k") {
		t.Error("Persist stub = true, want false until T3.01")
	}
	if _, st := s.TTLms("k"); st != NoKey {
		t.Errorf("TTLms stub status = %v, want NoKey until T3.01", st)
	}
	if s.SweepExpired(20, time.Millisecond) != 0 {
		t.Error("SweepExpired stub != 0, want 0 until T3.02")
	}
	if s.Snapshot() != nil {
		t.Error("Snapshot stub != nil, want nil until T6.02")
	}
}

func TestFakeClockMakesExpiryDeterministic(t *testing.T) {
	s, clock := newTestStore()
	putString(s, "k", "v", clock.NowMs()+1000)
	clock.Advance(999 * time.Millisecond)
	if _, ok := s.Lookup("k"); !ok {
		t.Fatal("key expired 1ms early")
	}
	clock.Advance(time.Millisecond)
	if _, ok := s.Lookup("k"); ok {
		t.Fatal("key still present at expiry instant; ExpireAt <= NowMs must expire")
	}
}
