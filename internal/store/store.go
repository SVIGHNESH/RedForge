package store

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

// Mutable returns the entry for in-place mutation. Identical to Lookup for
// now; T6.02 adds copy-on-write here: while snapshotActive it replaces the
// live entry with a copy whose Val is Val.Clone() so the snapshot goroutine's
// shared entry is never mutated.
func (s *mapStore) Mutable(key string) (*Entry, bool) {
	return s.Lookup(key)
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

// Snapshot is a T6.02 stub: it will copy the top-level map, set
// snapshotActive and return the clone as a SnapshotView.
func (s *mapStore) Snapshot() SnapshotView { return nil }

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
