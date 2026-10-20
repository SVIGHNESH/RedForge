package snapshot

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/SVIGHNESH/RedForge/internal/store"
)

// sample builds a store with one key of every supported type, one of them
// expiring, plus an empty-string key and value.
func sample() store.Store {
	st := store.New(nil)
	st.Put("str", &store.Entry{Type: store.TString, Val: &store.StringVal{B: []byte("hello")}})
	st.Put("", &store.Entry{Type: store.TString, Val: &store.StringVal{B: []byte{}}})
	st.Put("ttl", &store.Entry{Type: store.TString, Val: &store.StringVal{B: []byte("v")}, ExpireAt: 4102444800000})
	l := &store.ListVal{}
	for _, x := range []string{"a", "b", "c"} {
		l.PushBack([]byte(x))
	}
	l.PushFront([]byte("z")) // exercise the ring offset
	st.Put("list", &store.Entry{Type: store.TList, Val: l})
	st.Put("hash", &store.Entry{Type: store.THash, Val: &store.HashVal{M: map[string][]byte{"f1": []byte("v1"), "f2": []byte("")}}})
	st.Put("set", &store.Entry{Type: store.TSet, Val: &store.SetVal{M: map[string]struct{}{"x": {}, "y": {}}}})
	return st
}

func render(k string, e *store.Entry) string {
	var v string
	switch x := e.Val.(type) {
	case *store.StringVal:
		v = fmt.Sprintf("%q", x.B)
	case *store.ListVal:
		v = fmt.Sprintf("%q", x.Range(0, -1))
	case *store.HashVal:
		var fs []string
		for f, b := range x.M {
			fs = append(fs, f+"="+string(b))
		}
		sort.Strings(fs)
		v = fmt.Sprint(fs)
	case *store.SetVal:
		var ms []string
		for m := range x.M {
			ms = append(ms, m)
		}
		sort.Strings(ms)
		v = fmt.Sprint(ms)
	}
	return fmt.Sprintf("%q t=%d exp=%d %s", k, e.Type, e.ExpireAt, v)
}

func renderSource(src Source) []string {
	var out []string
	src.ForEach(func(k string, e *store.Entry) bool { out = append(out, render(k, e)); return true })
	sort.Strings(out)
	return out
}

func encode(t *testing.T, maxSeq uint64, src Source) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := Write(&buf, maxSeq, src); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return buf.Bytes()
}

func TestRoundTripEveryType(t *testing.T) {
	src := sample()
	b := encode(t, 42, src)
	maxSeq, entries, err := Read(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if maxSeq != 42 {
		t.Fatalf("max_seq = %d, want 42", maxSeq)
	}
	var got []string
	for _, ke := range entries {
		got = append(got, render(ke.Key, ke.Entry))
	}
	sort.Strings(got)
	if want := renderSource(src); !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip\n %v\nwant\n %v", got, want)
	}
}

// ReadInto feeds a store directly; the result is identical to the source,
// expiry index included.
func TestReadIntoStore(t *testing.T) {
	src := sample()
	dst := store.New(nil)
	if _, err := ReadInto(bytes.NewReader(encode(t, 1, src)), dst.Put); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(renderSource(dst), renderSource(src)) {
		t.Fatal("ReadInto store differs from the source")
	}
	if dst.ExpiringLen() != 1 {
		t.Fatalf("ExpiringLen = %d, want 1", dst.ExpiringLen())
	}
}

func TestEmptySnapshot(t *testing.T) {
	b := encode(t, 0, store.New(nil))
	if len(b) != len(magic)+1+8+8+4 {
		t.Fatalf("empty snapshot is %d bytes", len(b))
	}
	maxSeq, entries, err := Read(bytes.NewReader(b))
	if err != nil || maxSeq != 0 || len(entries) != 0 {
		t.Fatalf("empty: %d %v %v", maxSeq, entries, err)
	}
}

// Flipping any single byte must make the snapshot unloadable. Flips in
// value bytes and in the CRC itself keep the structure intact, so they are
// caught by the checksum specifically.
func TestEveryFlippedByteIsRejected(t *testing.T) {
	b := encode(t, 7, sample())
	for i := range b {
		bad := append([]byte(nil), b...)
		bad[i] ^= 0x5a
		_, _, err := Read(bytes.NewReader(bad))
		if !errors.Is(err, ErrChecksum) && !errors.Is(err, ErrFormat) {
			t.Fatalf("flip at %d of %d: err = %v, want ErrChecksum or ErrFormat", i, len(b), err)
		}
	}
	// The max_seq field and the last CRC byte: structure intact, so the
	// checksum must be what catches them.
	for _, i := range []int{len(magic) + 1, len(b) - 1} {
		bad := append([]byte(nil), b...)
		bad[i] ^= 0x01
		if _, _, err := Read(bytes.NewReader(bad)); !errors.Is(err, ErrChecksum) {
			t.Fatalf("flip at %d: err = %v, want ErrChecksum", i, err)
		}
	}
}

func TestEveryTruncationIsRejected(t *testing.T) {
	b := encode(t, 7, sample())
	for n := 0; n < len(b); n++ {
		if _, _, err := Read(bytes.NewReader(b[:n])); err == nil {
			t.Fatalf("snapshot cut to %d of %d bytes loaded", n, len(b))
		}
	}
}

func TestTrailingBytesAreRejected(t *testing.T) {
	b := append(encode(t, 7, sample()), 0)
	if _, _, err := Read(bytes.NewReader(b)); !errors.Is(err, ErrFormat) {
		t.Fatalf("err = %v, want ErrFormat", err)
	}
}

func TestWrongMagicOrVersion(t *testing.T) {
	b := encode(t, 1, store.New(nil))
	for _, i := range []int{0, len(magic)} {
		bad := append([]byte(nil), b...)
		bad[i]++
		if _, _, err := Read(bytes.NewReader(bad)); !errors.Is(err, ErrFormat) {
			t.Errorf("byte %d changed: err = %v, want ErrFormat", i, err)
		}
	}
}

func TestSortedSetIsAnErrorUntilS2(t *testing.T) {
	st := store.New(nil)
	st.Put("z", &store.Entry{Type: store.TZSet, Val: &store.ZSetVal{}})
	if err := Write(&bytes.Buffer{}, 0, st); err == nil {
		t.Fatal("Write accepted a sorted set")
	}
}

func FuzzRead(f *testing.F) {
	var buf bytes.Buffer
	Write(&buf, 3, sample())
	f.Add(buf.Bytes())
	f.Add([]byte(magic))
	f.Fuzz(func(t *testing.T, b []byte) {
		Read(bytes.NewReader(b))
	})
}
