package store

import "time"

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
	// SweepDeleted is the active sweep's share of ExpiredKeys.
	SweepDeleted() int64
}

var _ Counters = (*mapStore)(nil)

// ExpiredKeys implements Counters.
func (s *mapStore) ExpiredKeys() int64 { return s.expiredKeys }

// SweepDeleted implements Counters.
func (s *mapStore) SweepDeleted() int64 { return s.sweepDeleted }

// SweepExpired is the active half of expiry, run from the loop tick so keys
// that are never touched again are still reclaimed. The algorithm is the one
// Redis uses: look at up to sampleSize keys from the expiring index, delete
// the expired ones, and sample again while more than 25 percent of a sample
// was expired, stopping once budget is spent. Go randomises where each range
// over a map starts, so ranging and stopping after sampleSize keys is a
// random sample.
//
// Expiry is judged by the injected clock; the budget is wall time, because it
// bounds how long the loop is unavailable to clients. Deletions are counted
// but never propagated (MASTER-PLAN Section 4.4).
// sweepElapsed measures how long the current sweep call has run. Tests swap it
// for a fake so the budget rule is checked by round count, not by how busy
// the machine happens to be.
var sweepElapsed = time.Since

func (s *mapStore) SweepExpired(sampleSize int, budget time.Duration) int {
	if sampleSize <= 0 {
		return 0
	}
	start := time.Now()
	deleted := 0
	for len(s.expires) > 0 {
		s.sweepRounds++
		now := s.clock.NowMs()
		sampled, expired := 0, 0
		for key, at := range s.expires {
			if at <= now {
				delete(s.m, key)
				delete(s.expires, key)
				expired++
			}
			sampled++
			if sampled == sampleSize {
				break
			}
		}
		deleted += expired
		if expired*4 <= sampled || sweepElapsed(start) >= budget {
			break
		}
	}
	s.expiredKeys += int64(deleted)
	s.sweepDeleted += int64(deleted)
	return deleted
}

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
