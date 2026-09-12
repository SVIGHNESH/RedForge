package resp

// Kind identifies which of the five RESP2 types a Value holds.
type Kind uint8

const (
	KindSimpleString Kind = iota + 1
	KindError
	KindInteger
	KindBulk
	KindArray
)

// Value is a parsed RESP2 frame. Str carries the payload of a simple string,
// an error or a bulk string; Int carries an integer; Array carries the
// elements of an array. Null marks the null bulk string ($-1) and the null
// array (*-1), which have no payload of their own.
type Value struct {
	Kind  Kind
	Str   []byte
	Int   int64
	Array []Value
	Null  bool
}
