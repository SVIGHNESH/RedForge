package store

// minListCap is the first allocation for a list that grows from empty.
const minListCap = 8

// ListVal is a deque over a ring-buffered slice. Push and pop at either end
// are O(1) amortised, which is what the synopsis Table 3.1 promises for
// LPUSH, RPUSH, LPOP, RPOP and LLEN; a plain slice would make LPUSH O(N).
//
// The live elements are buf[head], buf[head+1], ... wrapping at len(buf), n of
// them in total. The buffer doubles when full. It never shrinks: a list that
// grew large and then drained keeps its capacity until the key is deleted,
// which happens as soon as it is empty. Shrinking is outside the M5 scope.
//
// Elements are byte slices the list owns. Callers copy bytes out of the read
// buffer before pushing, and never edit an element in place.
type ListVal struct {
	buf  [][]byte
	head int
	n    int
}

// Len is the number of elements.
func (l *ListVal) Len() int { return l.n }

// idx maps a logical position 0..n-1 to a slot in buf.
func (l *ListVal) idx(i int) int {
	j := l.head + i
	if j >= len(l.buf) {
		j -= len(l.buf)
	}
	return j
}

// grow doubles the buffer, copying the live elements in at most two segments
// (the run up to the end of buf, then the wrapped run from slot 0) so they
// start at slot 0 of the new buffer.
func (l *ListVal) grow() {
	newCap := 2 * len(l.buf)
	if newCap < minListCap {
		newCap = minListCap
	}
	nb := make([][]byte, newCap)
	if l.n > 0 {
		end := l.head + l.n
		if end > len(l.buf) {
			end = len(l.buf)
		}
		first := copy(nb, l.buf[l.head:end])
		copy(nb[first:], l.buf[:l.n-first])
	}
	l.buf = nb
	l.head = 0
}

// PushFront inserts b before the first element.
func (l *ListVal) PushFront(b []byte) {
	if l.n == len(l.buf) {
		l.grow()
	}
	l.head--
	if l.head < 0 {
		l.head += len(l.buf)
	}
	l.buf[l.head] = b
	l.n++
}

// PushBack appends b after the last element.
func (l *ListVal) PushBack(b []byte) {
	if l.n == len(l.buf) {
		l.grow()
	}
	l.buf[l.idx(l.n)] = b
	l.n++
}

// PopFront removes and returns the first element, or false when empty.
func (l *ListVal) PopFront() ([]byte, bool) {
	if l.n == 0 {
		return nil, false
	}
	b := l.buf[l.head]
	l.buf[l.head] = nil // let the GC reclaim the popped bytes
	l.head = l.idx(1)
	l.n--
	if l.n == 0 {
		l.head = 0
	}
	return b, true
}

// PopBack removes and returns the last element, or false when empty.
func (l *ListVal) PopBack() ([]byte, bool) {
	if l.n == 0 {
		return nil, false
	}
	j := l.idx(l.n - 1)
	b := l.buf[j]
	l.buf[j] = nil
	l.n--
	if l.n == 0 {
		l.head = 0
	}
	return b, true
}

// At returns element i, 0 being the first. It panics when i is out of range,
// like a slice index; callers check Len first.
func (l *ListVal) At(i int) []byte {
	if i < 0 || i >= l.n {
		panic("store: ListVal index out of range")
	}
	return l.buf[l.idx(i)]
}

// Range returns elements start..stop inclusive with the LRANGE index rules:
// a negative index counts from the end (-1 is the last element), a start
// still below zero clamps to 0, a stop past the end clamps to the last
// element, and the result is empty when start > stop or start is past the
// end. The outer slice is new; element bytes are shared with the list, which
// is safe because elements are never edited in place. O(S+N).
func (l *ListVal) Range(start, stop int) [][]byte {
	if start < 0 {
		start += l.n
	}
	if stop < 0 {
		stop += l.n
	}
	if start < 0 {
		start = 0
	}
	if stop >= l.n {
		stop = l.n - 1
	}
	if start > stop || start >= l.n {
		return [][]byte{}
	}
	out := make([][]byte, 0, stop-start+1)
	for i := start; i <= stop; i++ {
		out = append(out, l.buf[l.idx(i)])
	}
	return out
}

// Clone returns an independent deque with its own buffer and its own copy of
// every element, so the snapshot writer never observes a later push or pop
// on the live list (MASTER-PLAN Section 4.6).
func (l *ListVal) Clone() Value {
	if l == nil {
		return nil
	}
	size := l.n
	if size < minListCap {
		size = minListCap
	}
	out := &ListVal{buf: make([][]byte, size), n: l.n}
	for i := 0; i < l.n; i++ {
		b := l.buf[l.idx(i)]
		cp := make([]byte, len(b))
		copy(cp, b)
		out.buf[i] = cp
	}
	return out
}
