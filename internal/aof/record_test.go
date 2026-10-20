package aof

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"testing"
)

func argv(s ...string) [][]byte {
	out := make([][]byte, len(s))
	for i, a := range s {
		out[i] = []byte(a)
	}
	return out
}

// threeRecords returns a header plus three records and the file offset where
// each record starts.
func threeRecords() ([]byte, []int64) {
	data := append([]byte(magic), Version)
	var starts []int64
	for i, cmd := range [][][]byte{
		argv("SET", "a", "1"),
		argv("PEXPIREAT", "a", "1700000100000"),
		argv("RPUSH", "list", "x", "y", "z"),
	} {
		starts = append(starts, int64(len(data)))
		data = Encode(data, uint64(i+1), cmd)
	}
	return data, starts
}

func openReader(t *testing.T, data []byte) *Reader {
	t.Helper()
	r := bytes.NewReader(data)
	if err := ReadHeader(r); err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	return NewReader(r, int64(HeaderSize))
}

func TestRoundTrip(t *testing.T) {
	data, _ := threeRecords()
	rd := openReader(t, data)
	want := []struct {
		seq  uint64
		args [][]byte
	}{
		{1, argv("SET", "a", "1")},
		{2, argv("PEXPIREAT", "a", "1700000100000")},
		{3, argv("RPUSH", "list", "x", "y", "z")},
	}
	for _, w := range want {
		seq, args, err := rd.Next()
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if seq != w.seq || !reflect.DeepEqual(args, w.args) {
			t.Fatalf("got seq %d %q, want %d %q", seq, args, w.seq, w.args)
		}
	}
	if _, _, err := rd.Next(); err != io.EOF {
		t.Fatalf("after last record err = %v, want io.EOF", err)
	}
	if got := rd.LastGoodOffset(); got != int64(len(data)) {
		t.Fatalf("LastGoodOffset at EOF = %d, want file length %d", got, len(data))
	}
}

// The payload is exactly the RESP array the propagated command would be sent
// as, which is what T5.02's xxd check relies on.
func TestPayloadIsRESPArray(t *testing.T) {
	rec := Encode(nil, 7, argv("SET", "a", "1"))
	want := "*3\r\n$3\r\nSET\r\n$1\r\na\r\n$1\r\n1\r\n"
	if got := string(rec[recordHeaderSize:]); got != want {
		t.Fatalf("payload = %q, want %q", got, want)
	}
}

// Every truncation point inside record 3 must yield records 1 and 2 and then
// ErrTorn with LastGoodOffset at record 3's start. This is the CR4 policy at
// the codec level.
func TestTruncationAtEveryOffset(t *testing.T) {
	data, starts := threeRecords()
	third := starts[2]
	for cut := third; cut < int64(len(data)); cut++ {
		rd := openReader(t, data[:cut])
		for i := 0; i < 2; i++ {
			if _, _, err := rd.Next(); err != nil {
				t.Fatalf("cut %d: record %d: %v", cut, i+1, err)
			}
		}
		_, _, err := rd.Next()
		if cut == third {
			if err != io.EOF {
				t.Fatalf("cut at record boundary %d: err = %v, want io.EOF", cut, err)
			}
			continue
		}
		if !errors.Is(err, ErrTorn) {
			t.Fatalf("cut %d: err = %v, want ErrTorn", cut, err)
		}
		if got := rd.LastGoodOffset(); got != third {
			t.Fatalf("cut %d: LastGoodOffset = %d, want %d", cut, got, third)
		}
	}
}

func TestFlippedPayloadByteIsCorrupt(t *testing.T) {
	data, starts := threeRecords()
	bad := append([]byte(nil), data...)
	bad[starts[1]+recordHeaderSize+3] ^= 0x01
	rd := openReader(t, bad)
	if _, _, err := rd.Next(); err != nil {
		t.Fatalf("record 1: %v", err)
	}
	if _, _, err := rd.Next(); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("record 2 err = %v, want ErrCorrupt", err)
	}
	if got := rd.LastGoodOffset(); got != starts[1] {
		t.Fatalf("LastGoodOffset = %d, want %d", got, starts[1])
	}
}

// A flipped bit in len must give ErrTorn or ErrCorrupt, never a panic or a
// huge allocation, for every bit of the field.
func TestFlippedLenNeverPanics(t *testing.T) {
	data, starts := threeRecords()
	for bit := 0; bit < 32; bit++ {
		bad := append([]byte(nil), data...)
		bad[starts[0]+8+int64(bit/8)] ^= 1 << (bit % 8)
		rd := openReader(t, bad)
		_, _, err := rd.Next()
		if !errors.Is(err, ErrTorn) && !errors.Is(err, ErrCorrupt) {
			t.Fatalf("bit %d: err = %v, want ErrTorn or ErrCorrupt", bit, err)
		}
	}
}

func TestOversizedLenRejectedBeforeAllocating(t *testing.T) {
	rec := Encode(nil, 1, argv("SET", "a", "1"))
	rec[8], rec[9], rec[10], rec[11] = 0xff, 0xff, 0xff, 0xff
	rd := NewReader(bytes.NewReader(rec), 0)
	if _, _, err := rd.Next(); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
}

func TestHeaderChecks(t *testing.T) {
	cases := map[string][]byte{
		"empty":       nil,
		"short":       []byte("RFA"),
		"wrong magic": []byte("RFSNAP"),
		"wrong ver":   append([]byte(magic), 2),
	}
	for name, b := range cases {
		if err := ReadHeader(bytes.NewReader(b)); !errors.Is(err, ErrBadHeader) {
			t.Errorf("%s: err = %v, want ErrBadHeader", name, err)
		}
	}
}

func FuzzReader(f *testing.F) {
	data, _ := threeRecords()
	f.Add(data[HeaderSize:])
	f.Add([]byte{})
	f.Add(Encode(nil, 1, argv("SET", "k", "v"))[:10])
	f.Fuzz(func(t *testing.T, b []byte) {
		rd := NewReader(bytes.NewReader(b), 0)
		for i := 0; i < 1000; i++ {
			_, _, err := rd.Next()
			if err != nil {
				if off := rd.LastGoodOffset(); off < 0 || off > int64(len(b)) {
					t.Fatalf("LastGoodOffset %d outside [0,%d]", off, len(b))
				}
				return
			}
		}
	})
}
