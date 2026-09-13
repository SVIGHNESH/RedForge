package resp

import "strconv"

// Reply is what a command handler returns. Handlers never touch a socket: the
// loop appends the reply into the client's write buffer and flushes once per
// event, so AppendTo must not allocate a new slice of its own.
type Reply interface {
	AppendTo(dst []byte) []byte
}

type simpleStringReply string

func (r simpleStringReply) AppendTo(dst []byte) []byte {
	dst = append(dst, '+')
	dst = append(dst, r...)
	return append(dst, '\r', '\n')
}

type errorReply string

func (r errorReply) AppendTo(dst []byte) []byte {
	dst = append(dst, '-')
	dst = append(dst, r...)
	return append(dst, '\r', '\n')
}

type integerReply int64

func (r integerReply) AppendTo(dst []byte) []byte {
	dst = append(dst, ':')
	dst = strconv.AppendInt(dst, int64(r), 10)
	return append(dst, '\r', '\n')
}

type bulkReply []byte

func (r bulkReply) AppendTo(dst []byte) []byte {
	dst = append(dst, '$')
	dst = strconv.AppendInt(dst, int64(len(r)), 10)
	dst = append(dst, '\r', '\n')
	dst = append(dst, r...)
	return append(dst, '\r', '\n')
}

type nullBulkReply struct{}

func (nullBulkReply) AppendTo(dst []byte) []byte { return append(dst, "$-1\r\n"...) }

type nullArrayReply struct{}

func (nullArrayReply) AppendTo(dst []byte) []byte { return append(dst, "*-1\r\n"...) }

type arrayReply []Reply

func (r arrayReply) AppendTo(dst []byte) []byte {
	dst = append(dst, '*')
	dst = strconv.AppendInt(dst, int64(len(r)), 10)
	dst = append(dst, '\r', '\n')
	for _, item := range r {
		dst = item.AppendTo(dst)
	}
	return dst
}

// bulkArrayReply is the common case, an array whose every element is a bulk
// string. Keeping it distinct avoids wrapping each element in an interface.
type bulkArrayReply [][]byte

func (r bulkArrayReply) AppendTo(dst []byte) []byte {
	dst = append(dst, '*')
	dst = strconv.AppendInt(dst, int64(len(r)), 10)
	dst = append(dst, '\r', '\n')
	for _, item := range r {
		dst = bulkReply(item).AppendTo(dst)
	}
	return dst
}

// SimpleString builds +s\r\n. The caller must not pass a string containing CR
// or LF; every call site in this server passes a constant.
func SimpleString(s string) Reply { return simpleStringReply(s) }

// Error builds -s\r\n. Use the wording from MASTER-PLAN Section 4.11 verbatim.
func Error(s string) Reply { return errorReply(s) }

// IsError reports whether r is an error reply. The dispatcher uses it to
// decide whether a pending write consumes a sequence number: error replies
// never advance the counter (MASTER-PLAN Section 4.4, T0.08).
func IsError(r Reply) bool {
	_, ok := r.(errorReply)
	return ok
}

// Integer builds :n\r\n.
func Integer(n int64) Reply { return integerReply(n) }

// Bulk builds $len\r\nb\r\n. It keeps the slice rather than copying it, so the
// caller must not mutate b before the reply is appended.
func Bulk(b []byte) Reply { return bulkReply(b) }

// BulkString is Bulk for a Go string.
func BulkString(s string) Reply { return bulkReply(s) }

// NullBulk is $-1\r\n, the reply for a missing key.
func NullBulk() Reply { return nullBulkReply{} }

// NullArray is *-1\r\n.
func NullArray() Reply { return nullArrayReply{} }

// EmptyArray is *0\r\n.
func EmptyArray() Reply { return arrayReply(nil) }

// Array builds an array of arbitrary replies.
func Array(items ...Reply) Reply { return arrayReply(items) }

// ArrayOfBulks builds an array whose elements are all bulk strings, which is
// what most collection replies are.
func ArrayOfBulks(items [][]byte) Reply { return bulkArrayReply(items) }

// Shared singletons for the replies sent most often.
var (
	OK   = SimpleString("OK")
	PONG = SimpleString("PONG")
	Nil  = NullBulk()
)
