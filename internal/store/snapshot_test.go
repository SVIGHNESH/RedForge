package store

import (
	"fmt"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"
)

func listOf(items ...string) *ListVal {
	l := &ListVal{}
	for _, x := range items {
		l.PushBack([]byte(x))
	}
	return l
}

func viewDump(v SnapshotView) map[string]string {
	out := map[string]string{}
	v.ForEach(func(k string, e *Entry) bool {
		switch x := e.Val.(type) {
		case *StringVal:
			out[k] = string(x.B)
		case *ListVal:
			out[k] = fmt.Sprintf("%q", x.Range(0, -1))
		case *HashVal:
			var fs []string
			for f, b := range x.M {
				fs = append(fs, f+"="+string(b))
			}
			sort.Strings(fs)
			out[k] = fmt.Sprint(fs)
		case *SetVal:
			var ms []string
			for m := range x.M {
				ms = append(ms, m)
			}
			sort.Strings(ms)
			out[k] = fmt.Sprint(ms)
		}
		out[k] += " exp=" + strconv.FormatInt(e.ExpireAt, 10)
		return true
	})
	return out
}

// The view is frozen at the moment of Snapshot: a list mutated through
// Mutable, a new key, a deleted key, a hash, a set and a TTL change after
// that moment are all invisible to it, while the live store sees them.
func TestSnapshotViewIsPointInTime(t *testing.T) {
	s, clock := newTestStore()
	s.Put("list", &Entry{Type: TList, Val: listOf("a", "b")})
	s.Put("hash", &Entry{Type: THash, Val: &HashVal{M: map[string][]byte{"f": []byte("1")}}})
	s.Put("set", &Entry{Type: TSet, Val: &SetVal{M: map[string]struct{}{"x": {}}}})
	putString(s, "doomed", "v", 0)
	putString(s, "ttl", "v", 0)

	view := s.Snapshot()
	before := viewDump(view)

	e, _ := s.Mutable("list")
	e.Val.(*ListVal).PushBack([]byte("c"))
	h, _ := s.Mutable("hash")
	h.Val.(*HashVal).M["f"] = []byte("2")
	h.Val.(*HashVal).M["g"] = []byte("3")
	st, _ := s.Mutable("set")
	delete(st.Val.(*SetVal).M, "x")
	putString(s, "new", "v", 0)
	s.Delete("doomed")
	s.SetExpireAt("ttl", clock.NowMs()+5000)

	if after := viewDump(view); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("view changed after mutations:\n before %v\n after  %v", before, after)
	}
	if _, ok := before["new"]; ok {
		t.Fatal("view has a key put after the snapshot")
	}
	if _, ok := before["doomed"]; !ok {
		t.Fatal("view lost a key deleted after the snapshot")
	}
	// The live store has every change.
	if l, _ := s.Lookup("list"); l.Val.(*ListVal).Len() != 3 {
		t.Fatal("live list did not get the push")
	}
	if s.Exists("doomed") || !s.Exists("new") {
		t.Fatal("live store missed the delete or the put")
	}
	view.Release()
}

// A key is cloned once per snapshot, not once per mutation, and not at all
// when no snapshot is active.
func TestMutableClonesOncePerSnapshot(t *testing.T) {
	s, _ := newTestStore()
	s.Put("list", &Entry{Type: TList, Val: listOf("a")})
	ms := s.(*mapStore)

	s.Mutable("list")
	if ms.cowClones != 0 {
		t.Fatalf("cloned %d times with no snapshot", ms.cowClones)
	}
	view := s.Snapshot()
	for i := 0; i < 5; i++ {
		e, _ := s.Mutable("list")
		e.Val.(*ListVal).PushBack([]byte("x"))
	}
	if ms.cowClones != 1 {
		t.Fatalf("cloned %d times during one snapshot, want 1", ms.cowClones)
	}
	view.Release()
	s.Mutable("list")
	if ms.cowClones != 1 {
		t.Fatalf("cloned after release: %d", ms.cowClones)
	}
	// A second snapshot clones again.
	view2 := s.Snapshot()
	s.Mutable("list")
	if ms.cowClones != 2 {
		t.Fatalf("second snapshot cloned %d in total, want 2", ms.cowClones)
	}
	view2.Release()
}

func TestReleaseIsIdempotentAndAllowsTheNextSnapshot(t *testing.T) {
	s, _ := newTestStore()
	v := s.Snapshot()
	v.Release()
	v.Release()
	v2 := s.Snapshot()
	v.Release() // a stale view must not end the newer snapshot
	if s.(*mapStore).snap == nil {
		t.Fatal("stale Release ended the active snapshot")
	}
	v2.Release()
	if s.(*mapStore).snap != nil {
		t.Fatal("Release did not end the snapshot")
	}
}

func TestSecondSnapshotWhileActivePanics(t *testing.T) {
	s, _ := newTestStore()
	v := s.Snapshot()
	defer v.Release()
	defer func() {
		if recover() == nil {
			t.Fatal("second Snapshot did not panic")
		}
	}()
	s.Snapshot()
}

// Run with -race: the view is read on another goroutine while the loop
// mutates through Mutable, Put, Delete and SetExpireAt. Copy-on-write is
// what keeps this race-free without a lock.
func TestSnapshotReadWhileMutatingIsRaceFree(t *testing.T) {
	s, clock := newTestStore()
	for i := 0; i < 200; i++ {
		k := strconv.Itoa(i)
		s.Put("l"+k, &Entry{Type: TList, Val: listOf("a", "b")})
		s.Put("h"+k, &Entry{Type: THash, Val: &HashVal{M: map[string][]byte{"f": []byte("v")}}})
		putString(s, "s"+k, "v", 0)
	}
	view := s.Snapshot()
	var wg sync.WaitGroup
	wg.Add(1)
	var seen int
	go func() {
		defer wg.Done()
		for round := 0; round < 20; round++ {
			view.ForEach(func(k string, e *Entry) bool {
				switch x := e.Val.(type) {
				case *ListVal:
					seen += len(x.Range(0, -1))
				case *HashVal:
					seen += len(x.M)
				case *StringVal:
					seen += len(x.B)
				}
				seen += int(e.ExpireAt)
				return true
			})
		}
	}()
	for i := 0; i < 200; i++ {
		k := strconv.Itoa(i)
		if e, ok := s.Mutable("l" + k); ok {
			e.Val.(*ListVal).PushFront([]byte("z"))
		}
		if e, ok := s.Mutable("h" + k); ok {
			e.Val.(*HashVal).M["g"] = []byte("w")
		}
		putString(s, "s"+k, "new", 0)
		s.Delete("l" + strconv.Itoa((i+1)%200))
		s.SetExpireAt("h"+k, clock.NowMs()+1000)
	}
	wg.Wait()
	view.Release()
	if seen == 0 {
		t.Fatal("reader saw nothing")
	}
}

// The clone of a large keyspace is measured for INFO last_bgsave_clone_ms.
func TestCloneDurationIsRecorded(t *testing.T) {
	s, _ := newTestStore()
	n := 100000
	if testing.Short() {
		n = 1000
	}
	for i := 0; i < n; i++ {
		putString(s, strconv.Itoa(i), "v", 0)
	}
	v := s.Snapshot()
	d := s.(CloneTimer).LastCloneDuration()
	v.Release()
	if d <= 0 {
		t.Fatalf("clone duration = %v", d)
	}
	t.Logf("cloned %d keys in %v", n, d)
}

// BenchmarkSnapshotClone measures the loop-thread cost of Snapshot at 10^6
// keys, the number the T6.02 PR reports.
func BenchmarkSnapshotClone(b *testing.B) {
	s, _ := newTestStore()
	for i := 0; i < 1_000_000; i++ {
		putString(s, strconv.Itoa(i), "v", 0)
	}
	b.ResetTimer()
	var total time.Duration
	for i := 0; i < b.N; i++ {
		v := s.Snapshot()
		total += s.(CloneTimer).LastCloneDuration()
		v.Release()
	}
	b.ReportMetric(float64(total.Milliseconds())/float64(b.N), "ms/clone")
}
