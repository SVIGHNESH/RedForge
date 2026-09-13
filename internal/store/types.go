package store

import "time"

// Type identifies which kind of value an Entry holds.
type Type uint8

const (
	TString Type = iota + 1
	TList
	THash
	TSet
	TZSet
)

// Value is anything storable under a key. Every implementation must provide a
// Clone for the copy-on-write path in MASTER-PLAN Section 4.6: while a
// snapshot is in progress Mutable clones the value so the writer goroutine
// never observes a mutation.
type Value interface {
	Clone() Value
}

// Entry is one keyspace record. ExpireAt is an absolute Unix millisecond
// timestamp, 0 meaning no expiry. No relative TTL is ever persisted or
// replicated (Proposal Section 6.3): EXPIRE is rewritten to PEXPIREAT before
// it reaches the log.
type Entry struct {
	Type     Type
	Val      Value
	ExpireAt int64
}

// TTLStatus is the second result of TTLms.
type TTLStatus uint8

const (
	// NoKey means the key does not exist (or has passively expired).
	NoKey TTLStatus = iota
	// NoExpire means the key exists and has no expiry.
	NoExpire
	// HasExpire means the key exists with an expiry; the remaining ms is in
	// the first result of TTLms.
	HasExpire
)

// SnapshotView is the point-in-time handle T6.02 hands to the snapshot writer
// goroutine. It is read-only and released on the loop thread when the writer
// signals completion (MASTER-PLAN Section 4.6).
type SnapshotView interface {
	ForEach(fn func(key string, e *Entry) bool)
	Release()
}

// Store is the frozen keyspace contract from MASTER-PLAN Section 4.3.
// Lookup and Mutable apply passive expiry: an expired key is deleted and
// reported absent. T3.01 fills in the expiry methods, T3.02 the sweep and
// T6.02 the snapshot.
type Store interface {
	// Lookup applies passive expiry and returns the live entry.
	Lookup(key string) (*Entry, bool)
	// Mutable returns the entry for in-place mutation. Identical to Lookup
	// for now; T6.02 adds copy-on-write cloning while a snapshot is active.
	Mutable(key string) (*Entry, bool)
	Put(key string, e *Entry)
	Delete(key string) bool
	Exists(key string) bool
	// Keys returns every live key matching the Redis glob pattern
	// (* ? [abc] \x). Expired keys are reclaimed and excluded.
	Keys(pattern string) []string
	SetExpireAt(key string, atMs int64) bool
	Persist(key string) bool
	TTLms(key string) (int64, TTLStatus)
	Len() int
	ExpiringLen() int
	SweepExpired(sampleSize int, budget time.Duration) int
	Snapshot() SnapshotView
	ForEach(fn func(key string, e *Entry) bool)
}

// StringVal wraps a string value. It is never mutated in place; INCR and SET
// replace it, so Clone returns the receiver itself.
type StringVal struct {
	B []byte
}

// Clone returns itself because strings are never mutated in place.
func (v *StringVal) Clone() Value {
	if v == nil {
		return nil
	}
	return v
}

// ListVal is the deque placeholder T4.01 fleshes out in list.go with a
// ring-buffered O(1) push and pop at both ends plus Range with Redis
// negative-index rules.
type ListVal struct{}

// Clone returns an independent empty list. T4.01 deep-copies the deque.
func (v *ListVal) Clone() Value {
	if v == nil {
		return nil
	}
	return &ListVal{}
}

// HashVal wraps a hash's field map.
type HashVal struct {
	M map[string][]byte
}

// Clone deep-copies the field map so the snapshot writer never sees a live
// mutation.
func (v *HashVal) Clone() Value {
	if v == nil {
		return nil
	}
	out := &HashVal{M: make(map[string][]byte, len(v.M))}
	for k, val := range v.M {
		cp := make([]byte, len(val))
		copy(cp, val)
		out.M[k] = cp
	}
	return out
}

// SetVal wraps a set's member map.
type SetVal struct {
	M map[string]struct{}
}

// Clone deep-copies the member map.
func (v *SetVal) Clone() Value {
	if v == nil {
		return nil
	}
	out := &SetVal{M: make(map[string]struct{}, len(v.M))}
	for k := range v.M {
		out.M[k] = struct{}{}
	}
	return out
}

// ZSetVal is the sorted-set placeholder for stretch S2. T9.01 replaces it
// with a skiplist plus a score map; until then it clones to itself.
type ZSetVal struct{}

// Clone returns an independent empty sorted set.
func (v *ZSetVal) Clone() Value {
	if v == nil {
		return nil
	}
	return &ZSetVal{}
}
