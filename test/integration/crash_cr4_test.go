package integration

import (
	"encoding/binary"
	"hash/crc32"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/SVIGHNESH/RedForge/internal/resp"
	"github.com/SVIGHNESH/RedForge/internal/testutil"
)

// CR4 (PROPOSAL 6.4, MASTER-PLAN 4.7): a record that is cut short or fails
// its checksum ends replay. The file is truncated to where that record began,
// the server starts, INFO reports one truncated record and its offset, and
// the server keeps appending correctly afterwards.

// cr4Keys is how many SET k<i> v<i> writes each CR4 test makes.
const cr4Keys = 50

// TestCR4TornLastRecord cuts 7 bytes off the end of the log, which lands
// inside the last record's payload.
func TestCR4TornLastRecord(t *testing.T) {
	p := testutil.Start(t, "--appendfsync", "always")
	writeKeys(t, p.Client(t), cr4Keys)
	p.Kill()

	path := filepath.Join(p.DataDir(), "appendonly.aof")
	recs := readRecords(t, path)
	last := recs[len(recs)-1]
	if last.key != "k"+strconv.Itoa(cr4Keys-1) {
		t.Fatalf("last log record writes %q, want k%d", last.key, cr4Keys-1)
	}
	torn := fileSize(t, path) - 7
	if err := os.Truncate(path, torn); err != nil {
		t.Fatal(err)
	}

	p.Restart(t)
	c := p.Client(t)
	c.MustNil(t, "GET", "k"+strconv.Itoa(cr4Keys-1))
	if got := c.MustBulk(t, "GET", "k"+strconv.Itoa(cr4Keys-2)); got != "v"+strconv.Itoa(cr4Keys-2) {
		t.Fatalf("GET k%d = %q after recovery", cr4Keys-2, got)
	}
	if n := infoInt(t, c, "persistence", "aof_truncated_records"); n != 1 {
		t.Fatalf("aof_truncated_records = %d, want 1", n)
	}
	off := infoInt(t, c, "persistence", "aof_last_truncation_offset")
	if off != last.off {
		t.Fatalf("aof_last_truncation_offset = %d, want %d (where the torn record began)", off, last.off)
	}
	if off <= 0 || off >= torn {
		t.Fatalf("aof_last_truncation_offset = %d, want between 0 and %d", off, torn)
	}
	if size := fileSize(t, path); size != off {
		t.Fatalf("log is %d bytes after recovery, want it truncated to the record boundary %d", size, off)
	}

	// The server must append cleanly after the truncation point.
	c.MustOK(t, "SET", "k"+strconv.Itoa(cr4Keys-1), "again")
	p.Kill()
	p.Restart(t)
	if got := p.Client(t).MustBulk(t, "GET", "k"+strconv.Itoa(cr4Keys-1)); got != "again" {
		t.Fatalf("GET k%d after append-then-crash = %q, want again", cr4Keys-1, got)
	}
}

// TestCR4CorruptMiddleRecord flips one payload byte in the middle record, so
// its CRC fails: every key written before it survives and every key from it
// on is gone, because replay stops at the first bad record.
func TestCR4CorruptMiddleRecord(t *testing.T) {
	p := testutil.Start(t, "--appendfsync", "always")
	writeKeys(t, p.Client(t), cr4Keys)
	p.Kill()

	path := filepath.Join(p.DataDir(), "appendonly.aof")
	recs := readRecords(t, path)
	bad := len(recs) / 2
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r := recs[bad]
	data[r.payloadOff+r.payloadLen/2] ^= 0xFF
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	p.Restart(t)
	c := p.Client(t)
	for i, rec := range recs {
		if i < bad {
			if got := c.MustBulk(t, "GET", rec.key); got != rec.value {
				t.Fatalf("GET %s = %q, want %q (written before the corrupt record)", rec.key, got, rec.value)
			}
			continue
		}
		c.MustNil(t, "GET", rec.key)
	}
	if n := infoInt(t, c, "persistence", "aof_truncated_records"); n != 1 {
		t.Fatalf("aof_truncated_records = %d, want 1", n)
	}
	if off := infoInt(t, c, "persistence", "aof_last_truncation_offset"); off != r.off {
		t.Fatalf("aof_last_truncation_offset = %d, want %d (where the corrupt record began)", off, r.off)
	}
}

func writeKeys(t *testing.T, c *testutil.Client, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		c.MustOK(t, "SET", "k"+strconv.Itoa(i), "v"+strconv.Itoa(i))
	}
}

// aofRecord is one log record located by the MASTER-PLAN 4.7 layout.
type aofRecord struct {
	off        int64 // where the record header starts
	payloadOff int
	payloadLen int
	key, value string // from the SET k v payload
}

// aofHeaderLen is "RFAOF" plus the version byte.
const aofHeaderLen = 6

// readRecords walks a log written by the server using only the documented
// format: "RFAOF" + version u8, then records of seq u64 | len u32 | crc32 u32
// | payload, little-endian, CRC-32 IEEE over seq || len || payload. It fails
// the test if the file does not match the contract.
func readRecords(t *testing.T, path string) []aofRecord {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if len(data) < aofHeaderLen || string(data[:5]) != "RFAOF" || data[5] != 1 {
		t.Fatalf("log does not start with RFAOF version 1: %q", data[:min(len(data), aofHeaderLen)])
	}
	var recs []aofRecord
	for pos := aofHeaderLen; pos < len(data); {
		if len(data)-pos < 16 {
			t.Fatalf("log has %d stray bytes at offset %d", len(data)-pos, pos)
		}
		n := int(binary.LittleEndian.Uint32(data[pos+8:]))
		sum := binary.LittleEndian.Uint32(data[pos+12:])
		end := pos + 16 + n
		if end > len(data) {
			t.Fatalf("record at %d claims %d payload bytes past the end of the file", pos, n)
		}
		h := crc32.NewIEEE()
		h.Write(data[pos : pos+12])
		h.Write(data[pos+16 : end])
		if h.Sum32() != sum {
			t.Fatalf("record at %d fails its CRC", pos)
		}
		args, _, err := resp.ParseCommand(data[pos+16 : end])
		if err != nil || len(args) != 3 || string(args[0]) != "SET" {
			t.Fatalf("record at %d payload is not SET k v: %q", pos, data[pos+16:end])
		}
		recs = append(recs, aofRecord{
			off: int64(pos), payloadOff: pos + 16, payloadLen: n,
			key: string(args[1]), value: string(args[2]),
		})
		pos = end
	}
	if len(recs) != cr4Keys {
		t.Fatalf("log holds %d records, want one per SET (%d)", len(recs), cr4Keys)
	}
	return recs
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}
