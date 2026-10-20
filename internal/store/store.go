package store

import (
	"maps"
	"time"
)

// mapStore is the single Store implementation for T0.07: two maps owned by
// the loop thread, so no lock is needed. m holds every key; expires mirrors
// the subset with ExpireAt != 0 so T3.02 can sample expiring keys without
// scanning the whole keyspace.
type mapStore struct {
	m              map[string]*Entry
	expires        map[string]int64
	clock          Clock
	snapshotActive bool

	// expiredKeys counts keys reclaimed because their expiry passed, by the
	// passive path or the sweep. INFO reports it as expired_keys.
	expiredKeys int64
	// sweepDeleted counts only the sweep's share of expiredKeys, and
	// sweepRounds how many samples the sweep has taken; tests use the latter
	// to prove an idle sweep stops after one sample.
	sweepDeleted int64
	sweepRounds  int64

	// snap is the top-level map handed to the active snapshot, nil when
	// none is active; snapGen numbers snapshots so a stale view's Release
	// cannot end a newer one. lastClone is how long the last clone took and
	// cowClones how many values Mutable has cloned, for tests and INFO.
	snap      map[string]*Entry
	snapGen   uint64
	lastClone time.Duration
	cowClones int64

	// loading is set while recovery replays the log. Passive expiry and
	// the sweep are off, so a record replayed after its key's deadline
	// acts on the key exactly as it did when it was written; recovery
	// drops the due keys afterwards with DropExpired.
	loading bool
}

// Compile-time proof that the frozen interface is satisfied.
var _ Store = (*mapStore)(nil)

// New returns an empty map-backed Store reading time from clock. A nil clock
// means the wall clock.
func New(clock Clock) Store {
	if clock == nil {
		clock = RealClock{}
	}
	return &mapStore{
		m:       make(map[string]*Entry),
		expires: make(map[string]int64),
		clock:   clock,
	}
}

// Lookup returns the live entry for key, reclaiming it first when expired.
func (s *mapStore) Lookup(key string) (*Entry, bool) {
	e, ok := s.m[key]
	if !ok {
		return nil, false
	}
	if s.expireIfDue(key, e) {
		return nil, false
	}
	return e, true
}

// Mutable returns the entry for in-place mutation. While a snapshot is
// active, an entry still shared with it is first replaced in the live map by
// a copy whose Val is Val.Clone(), so the snapshot goroutine's entry is never
// mutated (MASTER-PLAN 4.6). A key is cloned at most once per snapshot:
// after the copy the live entry is no longer the snapshot's, so the pointer
// comparison fails on the next call.
func (s *mapStore) Mutable(key string) (*Entry, bool) {
	e, ok := s.Lookup(key)
	if !ok || s.snap == nil || s.snap[key] != e {
		return e, ok
	}
	cp := *e
	cp.Val = e.Val.Clone()
	s.m[key] = &cp
	s.cowClones++
	return &cp, true
}

// Put inserts or replaces key. A replacement with ExpireAt == 0 clears any
// previous TTL, as Redis SET does; this keeps expires consistent for T3.01.
func (s *mapStore) Put(key string, e *Entry) {
	if e == nil {
		s.Delete(key)
		return
	}
	s.m[key] = e
	if e.ExpireAt != 0 {
		s.expires[key] = e.ExpireAt
	} else {
		delete(s.expires, key)
	}
}

// Delete removes key, reporting whether it was present.
func (s *mapStore) Delete(key string) bool {
	if _, ok := s.m[key]; !ok {
		return false
	}
	delete(s.m, key)
	delete(s.expires, key)
	return true
}

// Exists reports whether key is present and live, reclaiming it when expired.
func (s *mapStore) Exists(key string) bool {
	_, ok := s.Lookup(key)
	return ok
}

// Keys returns every live key matching the Redis glob pattern, unsorted as in
// Redis (callers sort only for display). Expired keys are reclaimed and
// excluded, which is passive expiry during enumeration (T2.03).
func (s *mapStore) Keys(pattern string) []string {
	var out []string
	for key, e := range s.m {
		if s.expireIfDue(key, e) {
			continue
		}
		if Match(pattern, key) {
			out = append(out, key)
		}
	}
	return out
}

// Len is the number of stored keys, including expired-but-unvisited ones until
// the sweep reclaims them.
func (s *mapStore) Len() int { return len(s.m) }

// ExpiringLen is the number of keys carrying an expiry.
func (s *mapStore) ExpiringLen() int { return len(s.expires) }

// Snapshot hands out a point-in-time view for the BGSAVE writer goroutine
// (MASTER-PLAN Section 4.6). On the loop thread it copies the top-level map
// into a new one; the entries are shared, not copied. Until the view is
// released, Mutable clones a shared value before returning it, and Put,
// Delete, SetExpireAt and Persist replace entries in the live map rather than
// editing them, so the goroutine reading the view never observes a change
// and needs no lock.
//
// Only one snapshot may be active at a time; BGSAVE refuses a second save
// before it gets here, so a second call is a programming error and panics.
func (s *mapStore) Snapshot() SnapshotView {
	if s.snap != nil {
		panic("store: Snapshot called while another snapshot is active")
	}
	start := time.Now()
	// maps.Clone copies the hash table's buckets directly rather than
	// re-inserting every key, several times faster than a range-and-insert
	// loop on a large keyspace.
	clone := maps.Clone(s.m)
	if clone == nil {
		clone = map[string]*Entry{}
	}
	s.lastClone = time.Since(start)
	s.snap = clone
	s.snapGen++
	s.snapshotActive = true
	return &snapshotView{m: clone, store: s, gen: s.snapGen}
}

// snapshotView is the frozen map a snapshot writer iterates. Its map is
// never written after Snapshot returns, and every entry in it is either
// immutable or no longer reachable from the live map for mutation.
type snapshotView struct {
	m     map[string]*Entry
	store *mapStore
	gen   uint64
}

// ForEach visits every entry of the snapshot in map order until fn returns
// false. It is safe to call from the snapshot goroutine.
func (v *snapshotView) ForEach(fn func(key string, e *Entry) bool) {
	for k, e := range v.m {
		if !fn(k, e) {
			return
		}
	}
}

// Release ends copy-on-write and drops the store's reference to the clone.
// It must run on the loop thread, after the writer goroutine is done with
// the view. Releasing twice, or releasing a view that a later snapshot has
// replaced, is harmless.
func (v *snapshotView) Release() {
	s := v.store
	if s != nil && s.snap != nil && s.snapGen == v.gen {
		s.snap = nil
		s.snapshotActive = false
	}
	v.store = nil
}

// LastCloneDuration is how long the most recent Snapshot spent copying the
// top-level map on the loop thread. INFO reports it as last_bgsave_clone_ms.
func (s *mapStore) LastCloneDuration() time.Duration { return s.lastClone }

// CloneTimer is implemented by the store from New. Like Counters it sits
// outside the frozen Store interface and is reached by a type assertion.
type CloneTimer interface {
	LastCloneDuration() time.Duration
}

var _ CloneTimer = (*mapStore)(nil)

// ForEach visits every stored entry in map order until fn returns false. It
// does not apply passive expiry, so a caller that must hide expired keys
// checks ExpireAt itself or uses Lookup/Keys; this keeps a snapshot-style scan
// from mutating the map under the callback.
func (s *mapStore) ForEach(fn func(key string, e *Entry) bool) {
	for key, e := range s.m {
		if !fn(key, e) {
			return
		}
	}
}
