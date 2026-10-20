package store

import (
	"fmt"
	"math/rand"
	"testing"
)

// refRange is LRANGE over a plain slice, written independently of
// ListVal.Range so the two can be compared.
func refRange(ref []string, start, stop int) []string {
	n := len(ref)
	if start < 0 {
		start = n + start
		if start < 0 {
			start = 0
		}
	}
	if stop < 0 {
		stop = n + stop
	}
	if stop > n-1 {
		stop = n - 1
	}
	out := []string{}
	for i := start; i <= stop && i < n; i++ {
		out = append(out, ref[i])
	}
	return out
}

func toStrings(bs [][]byte) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = string(b)
	}
	return out
}

func listContents(l *ListVal) []string {
	out := make([]string, l.Len())
	for i := range out {
		out[i] = string(l.At(i))
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestListEmpty(t *testing.T) {
	l := &ListVal{}
	if l.Len() != 0 {
		t.Fatalf("Len = %d, want 0", l.Len())
	}
	if _, ok := l.PopFront(); ok {
		t.Fatal("PopFront on empty list reported a value")
	}
	if _, ok := l.PopBack(); ok {
		t.Fatal("PopBack on empty list reported a value")
	}
	if got := l.Range(0, -1); len(got) != 0 || got == nil {
		t.Fatalf("Range on empty list = %v, want non-nil empty", got)
	}
}

// Pushing at the front and popping at the back walks head around the ring
// many times; the order must survive every wraparound and every growth.
func TestListWraparoundBothEnds(t *testing.T) {
	l := &ListVal{}
	var ref []string
	next := 0
	for round := 0; round < 50; round++ {
		// Grow by a few, shrink by slightly fewer, so head keeps moving and
		// the buffer resizes while wrapped.
		for i := 0; i < 7; i++ {
			v := fmt.Sprint(next)
			next++
			if (round+i)%2 == 0 {
				l.PushFront([]byte(v))
				ref = append([]string{v}, ref...)
			} else {
				l.PushBack([]byte(v))
				ref = append(ref, v)
			}
		}
		for i := 0; i < 5; i++ {
			if (round+i)%3 == 0 {
				got, _ := l.PopBack()
				if string(got) != ref[len(ref)-1] {
					t.Fatalf("round %d PopBack = %q, want %q", round, got, ref[len(ref)-1])
				}
				ref = ref[:len(ref)-1]
			} else {
				got, _ := l.PopFront()
				if string(got) != ref[0] {
					t.Fatalf("round %d PopFront = %q, want %q", round, got, ref[0])
				}
				ref = ref[1:]
			}
		}
		if !equalStrings(listContents(l), ref) {
			t.Fatalf("round %d contents = %v, want %v", round, listContents(l), ref)
		}
	}
}

// Growth must happen while the live run is split across the end of buf, which
// is the two-segment copy path in grow.
func TestListGrowWhileWrapped(t *testing.T) {
	l := &ListVal{}
	for i := 0; i < minListCap; i++ {
		l.PushBack([]byte{byte('a' + i)})
	}
	l.PopFront()
	l.PopFront()
	l.PushBack([]byte("x"))
	l.PushBack([]byte("y")) // buffer is full and wrapped here
	if l.head == 0 {
		t.Fatal("test setup did not wrap the ring")
	}
	l.PushBack([]byte("z")) // forces grow on a wrapped buffer
	want := []string{"c", "d", "e", "f", "g", "h", "x", "y", "z"}
	if got := listContents(l); !equalStrings(got, want) {
		t.Fatalf("after wrapped grow = %v, want %v", got, want)
	}
}

// Every combination of positive, negative and out-of-range start and stop
// must agree with the slice reference, at several lengths and ring offsets.
func TestListRangeMatchesReference(t *testing.T) {
	for n := 0; n <= 12; n++ {
		for offset := 0; offset < 5; offset++ {
			l := &ListVal{}
			// Rotate head away from slot 0 so Range has to wrap.
			for i := 0; i < offset; i++ {
				l.PushBack([]byte("pad"))
				l.PopFront()
			}
			ref := make([]string, n)
			for i := 0; i < n; i++ {
				ref[i] = fmt.Sprint(i)
				l.PushBack([]byte(ref[i]))
			}
			for start := -n - 3; start <= n+3; start++ {
				for stop := -n - 3; stop <= n+3; stop++ {
					got := toStrings(l.Range(start, stop))
					want := refRange(ref, start, stop)
					if !equalStrings(got, want) {
						t.Fatalf("n=%d offset=%d Range(%d,%d) = %v, want %v", n, offset, start, stop, got, want)
					}
				}
			}
		}
	}
}

// 10000 random operations against a slice-backed model: after every step the
// length and full contents must match.
func TestListRandomOpsMatchReference(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	l := &ListVal{}
	var ref []string
	for step := 0; step < 10000; step++ {
		v := fmt.Sprint(step)
		switch rng.Intn(5) {
		case 0:
			l.PushFront([]byte(v))
			ref = append([]string{v}, ref...)
		case 1:
			l.PushBack([]byte(v))
			ref = append(ref, v)
		case 2:
			got, ok := l.PopFront()
			if len(ref) == 0 {
				if ok {
					t.Fatalf("step %d PopFront on empty returned %q", step, got)
				}
				continue
			}
			if !ok || string(got) != ref[0] {
				t.Fatalf("step %d PopFront = %q,%v, want %q", step, got, ok, ref[0])
			}
			ref = ref[1:]
		case 3:
			got, ok := l.PopBack()
			if len(ref) == 0 {
				if ok {
					t.Fatalf("step %d PopBack on empty returned %q", step, got)
				}
				continue
			}
			if !ok || string(got) != ref[len(ref)-1] {
				t.Fatalf("step %d PopBack = %q,%v, want %q", step, got, ok, ref[len(ref)-1])
			}
			ref = ref[:len(ref)-1]
		case 4:
			start := rng.Intn(2*len(ref)+5) - len(ref) - 2
			stop := rng.Intn(2*len(ref)+5) - len(ref) - 2
			got := toStrings(l.Range(start, stop))
			if want := refRange(ref, start, stop); !equalStrings(got, want) {
				t.Fatalf("step %d Range(%d,%d) = %v, want %v", step, start, stop, got, want)
			}
		}
		if l.Len() != len(ref) {
			t.Fatalf("step %d Len = %d, want %d", step, l.Len(), len(ref))
		}
	}
	if !equalStrings(listContents(l), ref) {
		t.Fatal("final contents differ from reference")
	}
}

// The clone is what the snapshot writer reads, so no mutation of the
// original may show through it, and no mutation of the clone may show
// through the original.
func TestListCloneIsIndependent(t *testing.T) {
	l := &ListVal{}
	for i := 0; i < 20; i++ {
		l.PushFront([]byte(fmt.Sprint(i)))
	}
	l.PopBack() // make sure head is not at slot 0
	want := listContents(l)

	c, ok := l.Clone().(*ListVal)
	if !ok {
		t.Fatal("Clone returned the wrong type")
	}
	if !equalStrings(listContents(c), want) {
		t.Fatalf("clone = %v, want %v", listContents(c), want)
	}

	c.PushFront([]byte("new"))
	c.PopBack()
	c.At(0)[0] = 'X' // even an in-place byte edit on the clone stays local
	if got := listContents(l); !equalStrings(got, want) {
		t.Fatalf("original changed after clone mutation: %v, want %v", got, want)
	}

	l.PushBack([]byte("live"))
	if c.Len() != len(want) {
		t.Fatalf("clone Len = %d after original grew, want %d", c.Len(), len(want))
	}
	if (*ListVal)(nil).Clone() != nil {
		t.Fatal("nil ListVal must clone to nil")
	}
}

func TestListAtPanicsOutOfRange(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("At(1) on a one-element list did not panic")
		}
	}()
	l := &ListVal{}
	l.PushBack([]byte("a"))
	l.At(1)
}

// BenchmarkPushFront pushes b.N elements onto lists pre-filled to each size.
// ns/op should stay flat from 10^3 to 10^6, which is the O(1) amortised
// claim in synopsis Table 3.1.
func BenchmarkPushFront(b *testing.B) {
	item := []byte("v")
	for _, size := range []int{1000, 10000, 100000, 1000000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			l := &ListVal{}
			for i := 0; i < size; i++ {
				l.PushFront(item)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				l.PushFront(item)
				l.PopBack()
			}
		})
	}
}
