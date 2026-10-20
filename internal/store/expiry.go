package store

// Expiry for M4. Every expiry is an absolute Unix millisecond timestamp in
// Entry.ExpireAt, mirrored in s.expires so the sweep can sample expiring
// keys without walking the whole keyspace. No relative TTL is stored
// anywhere (Proposal Section 6.3).
//
// An entry may be shared with a snapshot clone (MASTER-PLAN Section 4.6), so
// changing a key's expiry replaces the *Entry in the live map with a copy
// instead of writing ExpireAt through the shared pointer.

// Counters exposes store statistics for INFO. The store returned by New
// implements it. It is deliberately not part of the frozen Store interface
// (MASTER-PLAN Section 4.3), so INFO reads it with a type assertion and a new
// counter never needs a contract change.
type Counters interface {
	// ExpiredKeys is how many keys were reclaimed because their expiry
	// passed, whether found by a lookup or by the active sweep.
	ExpiredKeys() int64
}

var _ Counters = (*mapStore)(nil)

// ExpiredKeys implements Counters.
func (s *mapStore) ExpiredKeys() int64 { return s.expiredKeys }

// expireIfDue deletes key when its expiry has passed and reports whether it
// did. Lookup, Mutable, Exists and Keys all funnel through here, which is the
// passive half of expiry. The deletion is never propagated: replay and
// replicas drop the key from the same absolute timestamp (MASTER-PLAN 4.4).
func (s *mapStore) expireIfDue(key string, e *Entry) bool {
	if e == nil || e.ExpireAt <= 0 {
		return false
	}
	if e.ExpireAt > s.clock.NowMs() {
		return false
	}
	delete(s.m, key)
	delete(s.expires, key)
	s.expiredKeys++
	return true
}

// SetExpireAt gives a live key the absolute expiry atMs (Unix ms, must be
// positive) and reports whether the key existed. A time at or before now is
// stored as given, and the key is reclaimed on its next access; EXPIRE
// deletes such keys itself so that the deletion is propagated as DEL.
func (s *mapStore) SetExpireAt(key string, atMs int64) bool {
	if atMs <= 0 {
		return false
	}
	e, ok := s.Lookup(key)
	if !ok {
		return false
	}
	cp := *e
	cp.ExpireAt = atMs
	s.m[key] = &cp
	s.expires[key] = atMs
	return true
}

// Persist removes the expiry of a live key and reports whether there was
// one to remove.
func (s *mapStore) Persist(key string) bool {
	e, ok := s.Lookup(key)
	if !ok || e.ExpireAt == 0 {
		return false
	}
	cp := *e
	cp.ExpireAt = 0
	s.m[key] = &cp
	delete(s.expires, key)
	return true
}

// TTLms reports the remaining lifetime of key in milliseconds. The status
// is NoKey for a missing or just-expired key, NoExpire for a key without an
// expiry, and HasExpire otherwise, in which case the duration is positive.
func (s *mapStore) TTLms(key string) (int64, TTLStatus) {
	e, ok := s.Lookup(key)
	if !ok {
		return 0, NoKey
	}
	if e.ExpireAt == 0 {
		return 0, NoExpire
	}
	return e.ExpireAt - s.clock.NowMs(), HasExpire
}
