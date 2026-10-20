package aof

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"

	"github.com/SVIGHNESH/RedForge/internal/resp"
)

// File format from MASTER-PLAN Section 4.7, little-endian throughout:
//
//	file header   "RFAOF" + version u8 (=1)          written once on create
//	record        seq u64 | len u32 | crc32 u32 | payload[len]
//	payload       RESP2 array of the propagated command
//	crc32         IEEE over seq || len || payload
const (
	magic = "RFAOF"
	// Version is the only log format version this build reads or writes.
	Version = 1
	// HeaderSize is the length of the file header in bytes.
	HeaderSize = len(magic) + 1
	// recordHeaderSize is seq (8) + len (4) + crc (4).
	recordHeaderSize = 16
	// MaxPayload caps a record's declared length before anything is
	// allocated, so a corrupt len field cannot ask for gigabytes.
	MaxPayload = 512 << 20
)

var (
	// ErrTorn means the log ends partway through a record: the header or
	// the payload is shorter than it claims. It is the signature of a crash
	// during a write.
	ErrTorn = errors.New("aof: torn record")
	// ErrCorrupt means a complete record failed its checksum or does not
	// hold a valid RESP command.
	ErrCorrupt = errors.New("aof: corrupt record")
	// ErrBadHeader means the file does not start with the RFAOF header or
	// carries a version this build does not understand.
	ErrBadHeader = errors.New("aof: bad file header")
)

// Encode appends one record for seq and the command args to dst and returns
// the extended slice.
func Encode(dst []byte, seq uint64, args [][]byte) []byte {
	start := len(dst)
	dst = append(dst, make([]byte, recordHeaderSize)...)
	dst = resp.ArrayOfBulks(args).AppendTo(dst)
	payload := dst[start+recordHeaderSize:]

	hdr := dst[start : start+recordHeaderSize]
	binary.LittleEndian.PutUint64(hdr[0:8], seq)
	binary.LittleEndian.PutUint32(hdr[8:12], uint32(len(payload)))
	binary.LittleEndian.PutUint32(hdr[12:16], checksum(hdr[0:12], payload))
	return dst
}

// checksum is CRC32 IEEE over seq || len || payload.
func checksum(seqAndLen, payload []byte) uint32 {
	c := crc32.Update(0, crc32.IEEETable, seqAndLen)
	return crc32.Update(c, crc32.IEEETable, payload)
}

// WriteHeader writes the file header.
func WriteHeader(w io.Writer) error {
	_, err := w.Write(append([]byte(magic), Version))
	return err
}

// ReadHeader reads and checks the file header.
func ReadHeader(r io.Reader) error {
	var buf [HeaderSize]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return fmt.Errorf("%w: %v", ErrBadHeader, err)
	}
	if string(buf[:len(magic)]) != magic {
		return fmt.Errorf("%w: magic %q", ErrBadHeader, buf[:len(magic)])
	}
	if buf[len(magic)] != Version {
		return fmt.Errorf("%w: version %d", ErrBadHeader, buf[len(magic)])
	}
	return nil
}

// Reader decodes records one at a time. It does not read the file header;
// call ReadHeader first and pass the header length as the base offset so
// LastGoodOffset is a file offset.
type Reader struct {
	r        *bufio.Reader
	offset   int64 // offset of the next unread byte
	lastGood int64 // offset where the most recent record started
}

// NewReader returns a Reader over r whose first byte is at file offset base.
func NewReader(r io.Reader, base int64) *Reader {
	return &Reader{r: bufio.NewReaderSize(r, 64*1024), offset: base, lastGood: base}
}

// Next returns the next record. At a clean end of input on a record
// boundary it returns io.EOF. A short header or payload returns ErrTorn and a
// checksum or RESP mismatch returns ErrCorrupt; in both cases LastGoodOffset
// is the offset where the failing record starts, which is where the file
// must be truncated.
func (rd *Reader) Next() (seq uint64, args [][]byte, err error) {
	rd.lastGood = rd.offset
	var hdr [recordHeaderSize]byte
	n, err := io.ReadFull(rd.r, hdr[:])
	rd.offset += int64(n)
	switch {
	case err == io.EOF:
		return 0, nil, io.EOF
	case err == io.ErrUnexpectedEOF:
		return 0, nil, ErrTorn
	case err != nil:
		return 0, nil, err
	}
	seq = binary.LittleEndian.Uint64(hdr[0:8])
	size := binary.LittleEndian.Uint32(hdr[8:12])
	sum := binary.LittleEndian.Uint32(hdr[12:16])
	if size > MaxPayload {
		return 0, nil, ErrCorrupt
	}
	// Read in chunks so a corrupt but in-range length on a short file never
	// allocates more than the bytes actually present plus one chunk.
	payload, err := readUpTo(rd.r, int(size))
	rd.offset += int64(len(payload))
	if err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return 0, nil, ErrTorn
		}
		return 0, nil, err
	}
	if checksum(hdr[0:12], payload) != sum {
		return 0, nil, ErrCorrupt
	}
	args, used, perr := resp.ParseCommand(payload)
	if perr != nil || used != len(payload) || len(args) == 0 {
		return 0, nil, ErrCorrupt
	}
	return seq, args, nil
}

// readUpTo reads exactly n bytes, growing the buffer in bounded steps.
func readUpTo(r io.Reader, n int) ([]byte, error) {
	const step = 1 << 20
	buf := make([]byte, 0, min(n, step))
	for len(buf) < n {
		want := min(n-len(buf), step)
		if cap(buf)-len(buf) < want {
			grown := make([]byte, len(buf), len(buf)+want)
			copy(grown, buf)
			buf = grown
		}
		got, err := io.ReadFull(r, buf[len(buf):len(buf)+want])
		buf = buf[:len(buf)+got]
		if err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return buf, err
		}
	}
	return buf, nil
}

// LastGoodOffset is the file offset where the most recently attempted record
// starts. After ErrTorn or ErrCorrupt it is the length the file should be
// truncated to; after io.EOF it is the file length.
func (rd *Reader) LastGoodOffset() int64 { return rd.lastGood }
