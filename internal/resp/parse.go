package resp

import (
	"bytes"
	"errors"
	"strconv"
)

// ErrIncomplete means the buffer does not yet hold a whole frame. The caller
// keeps the bytes and retries after the next read; this is what makes partial
// reads and pipelining work in the event loop.
var ErrIncomplete = errors.New("resp: incomplete frame")

// Limits mirror Redis: a bulk string may not exceed 512 MB and an array may
// not declare more than 1024*1024 elements. Both guard rbuf growth.
const (
	MaxBulkLength  = 512 * 1024 * 1024
	MaxArrayLength = 1024 * 1024
	// MaxInlineLength bounds an inline command line, as Redis does, so a
	// client that never sends \r\n cannot grow the read buffer forever.
	MaxInlineLength = 64 * 1024
	// MaxDepth bounds array nesting. Parsing is recursive, so without this a
	// client could send "*1\r\n" a million times and overflow the loop's stack.
	MaxDepth = 32
)

// ProtocolError is a malformed frame. Its message always starts with
// "Protocol error:" so the server can write it back verbatim as an -ERR reply.
type ProtocolError struct{ msg string }

func (e *ProtocolError) Error() string { return "Protocol error: " + e.msg }

func protoErr(msg string) error { return &ProtocolError{msg: msg} }

// IsProtocolError reports whether err is a malformed-frame error.
func IsProtocolError(err error) bool {
	var pe *ProtocolError
	return errors.As(err, &pe)
}

// Parse reads one RESP2 frame from the front of buf. It returns the value and
// the number of bytes consumed. When the frame is not fully present it returns
// n == 0 and ErrIncomplete, leaving buf untouched.
func Parse(buf []byte) (v Value, n int, err error) {
	return parse(buf, 0)
}

func parse(buf []byte, depth int) (v Value, n int, err error) {
	if len(buf) == 0 {
		return Value{}, 0, ErrIncomplete
	}
	switch buf[0] {
	case '+':
		return parseLineValue(buf, KindSimpleString)
	case '-':
		return parseLineValue(buf, KindError)
	case ':':
		return parseInteger(buf)
	case '$':
		return parseBulk(buf)
	case '*':
		return parseArray(buf, depth)
	default:
		return parseInline(buf)
	}
}

// readLine returns the bytes up to the next CRLF and the total consumed count
// including the CRLF, starting the search at from.
func readLine(buf []byte, from int) (line []byte, n int, err error) {
	i := bytes.IndexByte(buf[from:], '\n')
	if i < 0 {
		return nil, 0, ErrIncomplete
	}
	end := from + i
	if end == 0 || buf[end-1] != '\r' {
		return nil, 0, protoErr("unbalanced line ending")
	}
	return buf[from : end-1], end + 1, nil
}

func parseLineValue(buf []byte, kind Kind) (Value, int, error) {
	line, n, err := readLine(buf, 1)
	if err != nil {
		return Value{}, 0, err
	}
	return Value{Kind: kind, Str: line}, n, nil
}

func parseInteger(buf []byte) (Value, int, error) {
	line, n, err := readLine(buf, 1)
	if err != nil {
		return Value{}, 0, err
	}
	i, perr := parseInt(line)
	if perr != nil {
		return Value{}, 0, protoErr("invalid integer")
	}
	return Value{Kind: KindInteger, Int: i}, n, nil
}

func parseBulk(buf []byte) (Value, int, error) {
	line, hdr, err := readLine(buf, 1)
	if err != nil {
		return Value{}, 0, err
	}
	length, perr := parseInt(line)
	if perr != nil {
		return Value{}, 0, protoErr("invalid bulk length")
	}
	if length == -1 {
		return Value{Kind: KindBulk, Null: true}, hdr, nil
	}
	if length < 0 || length > MaxBulkLength {
		return Value{}, 0, protoErr("invalid bulk length")
	}
	end := hdr + int(length) + 2
	if end > len(buf) {
		return Value{}, 0, ErrIncomplete
	}
	if buf[end-2] != '\r' || buf[end-1] != '\n' {
		return Value{}, 0, protoErr("unbalanced line ending")
	}
	return Value{Kind: KindBulk, Str: buf[hdr : end-2]}, end, nil
}

func parseArray(buf []byte, depth int) (Value, int, error) {
	if depth >= MaxDepth {
		return Value{}, 0, protoErr("invalid multibulk length")
	}
	line, n, err := readLine(buf, 1)
	if err != nil {
		return Value{}, 0, err
	}
	count, perr := parseInt(line)
	if perr != nil {
		return Value{}, 0, protoErr("invalid multibulk length")
	}
	if count == -1 {
		return Value{Kind: KindArray, Null: true}, n, nil
	}
	if count < 0 || count > MaxArrayLength {
		return Value{}, 0, protoErr("invalid multibulk length")
	}
	items := make([]Value, 0, count)
	for i := int64(0); i < count; i++ {
		item, used, ierr := parse(buf[n:], depth+1)
		if ierr != nil {
			return Value{}, 0, ierr
		}
		n += used
		items = append(items, item)
	}
	return Value{Kind: KindArray, Array: items}, n, nil
}

// parseInline handles the form nc and telnet send: a bare line split on
// whitespace, delivered as an array of bulk strings.
func parseInline(buf []byte) (Value, int, error) {
	i := bytes.IndexByte(buf, '\n')
	if i < 0 {
		if len(buf) > MaxInlineLength {
			return Value{}, 0, protoErr("too big inline request")
		}
		return Value{}, 0, ErrIncomplete
	}
	if i+1 > MaxInlineLength {
		return Value{}, 0, protoErr("too big inline request")
	}
	line := buf[:i]
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	if bytes.IndexByte(line, 0) >= 0 {
		return Value{}, 0, protoErr("unbalanced quotes in request")
	}
	items := []Value{}
	for _, f := range bytes.Fields(line) {
		items = append(items, Value{Kind: KindBulk, Str: f})
	}
	return Value{Kind: KindArray, Array: items}, i + 1, nil
}

// parseInt is strconv.ParseInt over a byte slice without allocating.
func parseInt(b []byte) (int64, error) {
	if len(b) == 0 {
		return 0, strconv.ErrSyntax
	}
	return strconv.ParseInt(string(b), 10, 64)
}

// ParseCommand reads one client command from the front of buf and returns its
// argument vector. Clients may send either an array of bulk strings or an
// inline line; anything else is a protocol error. An empty inline line yields
// nil args and a non-zero n, which the caller skips.
func ParseCommand(buf []byte) (args [][]byte, n int, err error) {
	v, n, err := Parse(buf)
	if err != nil {
		return nil, 0, err
	}
	if v.Kind != KindArray {
		return nil, 0, protoErr("expected '$', got '" + string(buf[0]) + "'")
	}
	if v.Null || len(v.Array) == 0 {
		return nil, n, nil
	}
	args = make([][]byte, 0, len(v.Array))
	for _, item := range v.Array {
		if item.Kind != KindBulk || item.Null {
			return nil, 0, protoErr("expected '$', got something else")
		}
		args = append(args, item.Str)
	}
	return args, n, nil
}
