package snapshot

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"

	"github.com/SVIGHNESH/RedForge/internal/store"
)

// File format from MASTER-PLAN Section 4.8, little-endian throughout:
//
//	"RFSNAP" + version u8 (=1)
//	max_seq   u64
//	count     u64
//	entries   count times: key (u32 len + bytes) | type u8 | expire_at i64 | value
//	value     STRING: u32 len + bytes
//	          LIST:   u32 n, then n items (u32 len + bytes) head to tail
//	          HASH:   u32 n, then n pairs (field, value)
//	          SET:    u32 n, then n members
//	          ZSET:   u32 n, then n pairs (member, score f64)   stretch S2
//	crc32     u32 IEEE over everything before it
const (
	magic = "RFSNAP"
	// Version is the only snapshot format version this build reads or writes.
	Version = 1
	// maxBlob caps any single declared length before it is allocated, so a
	// damaged length cannot ask for gigabytes before the CRC is checked.
	maxBlob = 512 << 20
)

var (
	// ErrChecksum means the file is complete but its trailing CRC does not
	// match: the snapshot is corrupt and must be ignored.
	ErrChecksum = errors.New("snapshot: checksum mismatch")
	// ErrFormat means the file is not a valid snapshot: wrong magic or
	// version, a truncated body, an unknown type, or bytes after the CRC.
	ErrFormat = errors.New("snapshot: bad format")
)

// Source is anything that can list entries: a store.SnapshotView during
// BGSAVE, or a store in tests.
type Source interface {
	ForEach(fn func(key string, e *store.Entry) bool)
}

// Write serialises every entry of src with maxSeq as the boundary. It walks
// src twice, once to count and once to write, so src must not change in
// between; a SnapshotView never does. A sorted set is an error until S2.
func Write(w io.Writer, maxSeq uint64, src Source) error {
	var count uint64
	src.ForEach(func(string, *store.Entry) bool { count++; return true })

	sum := crc32.NewIEEE()
	bw := bufio.NewWriterSize(io.MultiWriter(w, sum), 256*1024)
	enc := encoder{w: bw}
	enc.raw([]byte(magic))
	enc.raw([]byte{Version})
	enc.u64(maxSeq)
	enc.u64(count)

	var written uint64
	src.ForEach(func(key string, e *store.Entry) bool {
		enc.blob([]byte(key))
		enc.raw([]byte{byte(e.Type)})
		enc.u64(uint64(e.ExpireAt))
		enc.value(e)
		written++
		return enc.err == nil
	})
	if enc.err == nil && written != count {
		enc.err = fmt.Errorf("snapshot: source changed while writing (%d entries, counted %d)", written, count)
	}
	if enc.err != nil {
		return enc.err
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	var crc [4]byte
	binary.LittleEndian.PutUint32(crc[:], sum.Sum32())
	_, err := w.Write(crc[:])
	return err
}

type encoder struct {
	w   *bufio.Writer
	err error
	tmp [8]byte
}

func (e *encoder) raw(b []byte) {
	if e.err == nil {
		_, e.err = e.w.Write(b)
	}
}

func (e *encoder) u32(n uint32) {
	binary.LittleEndian.PutUint32(e.tmp[:4], n)
	e.raw(e.tmp[:4])
}

func (e *encoder) u64(n uint64) {
	binary.LittleEndian.PutUint64(e.tmp[:8], n)
	e.raw(e.tmp[:8])
}

func (e *encoder) blob(b []byte) {
	e.u32(uint32(len(b)))
	e.raw(b)
}

func (e *encoder) value(ent *store.Entry) {
	switch v := ent.Val.(type) {
	case *store.StringVal:
		e.blob(v.B)
	case *store.ListVal:
		e.u32(uint32(v.Len()))
		for i := 0; i < v.Len(); i++ {
			e.blob(v.At(i))
		}
	case *store.HashVal:
		e.u32(uint32(len(v.M)))
		for f, x := range v.M {
			e.blob([]byte(f))
			e.blob(x)
		}
	case *store.SetVal:
		e.u32(uint32(len(v.M)))
		for m := range v.M {
			e.blob([]byte(m))
		}
	default:
		if e.err == nil {
			e.err = fmt.Errorf("snapshot: cannot write value of type %d (%T)", ent.Type, ent.Val)
		}
	}
}

// KeyEntry is one decoded key.
type KeyEntry struct {
	Key   string
	Entry *store.Entry
}

// Read decodes a whole snapshot into memory. Prefer ReadInto for a store.
func Read(r io.Reader) (uint64, []KeyEntry, error) {
	var out []KeyEntry
	maxSeq, err := ReadInto(r, func(k string, e *store.Entry) { out = append(out, KeyEntry{k, e}) })
	if err != nil {
		return 0, nil, err
	}
	return maxSeq, out, nil
}

// ReadInto decodes a snapshot, calling put for each entry as it is read,
// and verifies the trailing CRC at the end. put may already have been called
// when ReadInto returns an error: the caller loads into an empty store and
// discards it on failure, as MASTER-PLAN 4.8 requires that a corrupt
// snapshot is ignored rather than partly applied.
func ReadInto(r io.Reader, put func(key string, e *store.Entry)) (uint64, error) {
	br := bufio.NewReaderSize(r, 256*1024)
	sum := crc32.NewIEEE()
	d := decoder{r: io.TeeReader(br, sum)}

	head := d.bytes(len(magic) + 1)
	if d.err != nil {
		return 0, fmt.Errorf("%w: header: %v", ErrFormat, d.err)
	}
	if string(head[:len(magic)]) != magic || head[len(magic)] != Version {
		return 0, fmt.Errorf("%w: header %q", ErrFormat, head)
	}
	maxSeq := d.u64()
	count := d.u64()
	for i := uint64(0); i < count && d.err == nil; i++ {
		key := string(d.blob())
		typ := store.Type(d.byte())
		exp := int64(d.u64())
		val := d.value(typ)
		if d.err == nil {
			put(key, &store.Entry{Type: typ, Val: val, ExpireAt: exp})
		}
	}
	if d.err != nil {
		if errors.Is(d.err, ErrFormat) {
			return 0, d.err
		}
		return 0, fmt.Errorf("%w: %v", ErrFormat, d.err)
	}
	want := sum.Sum32()
	var crc [4]byte
	if _, err := io.ReadFull(br, crc[:]); err != nil {
		return 0, fmt.Errorf("%w: missing checksum: %v", ErrFormat, err)
	}
	if binary.LittleEndian.Uint32(crc[:]) != want {
		return 0, ErrChecksum
	}
	if _, err := br.ReadByte(); err != io.EOF {
		return 0, fmt.Errorf("%w: bytes after the checksum", ErrFormat)
	}
	return maxSeq, nil
}

type decoder struct {
	r   io.Reader
	err error
	tmp [8]byte
}

// bytes reads exactly n bytes, growing the buffer 1 MB at a time so a
// damaged but in-range length on a short file fails at EOF instead of
// allocating the whole declared size first.
func (d *decoder) bytes(n int) []byte {
	if d.err != nil {
		return nil
	}
	const step = 1 << 20
	b := make([]byte, 0, min(n, step))
	for len(b) < n {
		want := min(n-len(b), step)
		if cap(b)-len(b) < want {
			grown := make([]byte, len(b), len(b)+want)
			copy(grown, b)
			b = grown
		}
		if _, err := io.ReadFull(d.r, b[len(b):len(b)+want]); err != nil {
			d.err = err
			return nil
		}
		b = b[:len(b)+want]
	}
	return b
}

func (d *decoder) byte() byte {
	b := d.bytes(1)
	if b == nil {
		return 0
	}
	return b[0]
}

func (d *decoder) u32() uint32 {
	if d.err != nil {
		return 0
	}
	if _, err := io.ReadFull(d.r, d.tmp[:4]); err != nil {
		d.err = err
		return 0
	}
	return binary.LittleEndian.Uint32(d.tmp[:4])
}

func (d *decoder) u64() uint64 {
	if d.err != nil {
		return 0
	}
	if _, err := io.ReadFull(d.r, d.tmp[:8]); err != nil {
		d.err = err
		return 0
	}
	return binary.LittleEndian.Uint64(d.tmp[:8])
}

func (d *decoder) blob() []byte {
	n := d.u32()
	if d.err == nil && n > maxBlob {
		d.err = fmt.Errorf("%w: length %d over the limit", ErrFormat, n)
	}
	return d.bytes(int(n))
}

// count reads a collection size. Allocation is bounded by what is actually
// read, so a damaged count fails on EOF instead of allocating.
func (d *decoder) count() int {
	n := d.u32()
	if d.err == nil && n > maxBlob {
		d.err = fmt.Errorf("%w: count %d over the limit", ErrFormat, n)
	}
	return int(n)
}

func (d *decoder) value(t store.Type) store.Value {
	switch t {
	case store.TString:
		return &store.StringVal{B: d.blob()}
	case store.TList:
		n := d.count()
		l := &store.ListVal{}
		for i := 0; i < n && d.err == nil; i++ {
			l.PushBack(d.blob())
		}
		return l
	case store.THash:
		n := d.count()
		h := &store.HashVal{M: make(map[string][]byte, min(n, 1024))}
		for i := 0; i < n && d.err == nil; i++ {
			f := string(d.blob())
			h.M[f] = d.blob()
		}
		return h
	case store.TSet:
		n := d.count()
		s := &store.SetVal{M: make(map[string]struct{}, min(n, 1024))}
		for i := 0; i < n && d.err == nil; i++ {
			s.M[string(d.blob())] = struct{}{}
		}
		return s
	}
	if d.err == nil {
		d.err = fmt.Errorf("%w: unknown value type %d", ErrFormat, t)
	}
	return nil
}
