package resp

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestParseTable(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Value
		n    int
	}{
		{"simple string", "+OK\r\n", Value{Kind: KindSimpleString, Str: []byte("OK")}, 5},
		{"empty simple string", "+\r\n", Value{Kind: KindSimpleString, Str: []byte("")}, 3},
		{"error", "-ERR boom\r\n", Value{Kind: KindError, Str: []byte("ERR boom")}, 11},
		{"integer", ":42\r\n", Value{Kind: KindInteger, Int: 42}, 5},
		{"negative integer", ":-7\r\n", Value{Kind: KindInteger, Int: -7}, 5},
		{"bulk", "$5\r\nhello\r\n", Value{Kind: KindBulk, Str: []byte("hello")}, 11},
		{"empty bulk", "$0\r\n\r\n", Value{Kind: KindBulk, Str: []byte("")}, 6},
		{"null bulk", "$-1\r\n", Value{Kind: KindBulk, Null: true}, 5},
		{"bulk with crlf inside", "$4\r\na\r\nb\r\n", Value{Kind: KindBulk, Str: []byte("a\r\nb")}, 10},
		{"null array", "*-1\r\n", Value{Kind: KindArray, Null: true}, 5},
		{"empty array", "*0\r\n", Value{Kind: KindArray, Array: []Value{}}, 4},
		{
			"array of bulks",
			"*2\r\n$4\r\nPING\r\n$3\r\nfoo\r\n",
			Value{Kind: KindArray, Array: []Value{
				{Kind: KindBulk, Str: []byte("PING")},
				{Kind: KindBulk, Str: []byte("foo")},
			}},
			23,
		},
		{
			"nested array",
			"*2\r\n*1\r\n:1\r\n+OK\r\n",
			Value{Kind: KindArray, Array: []Value{
				{Kind: KindArray, Array: []Value{{Kind: KindInteger, Int: 1}}},
				{Kind: KindSimpleString, Str: []byte("OK")},
			}},
			17,
		},
		{
			"inline",
			"PING foo\r\n",
			Value{Kind: KindArray, Array: []Value{
				{Kind: KindBulk, Str: []byte("PING")},
				{Kind: KindBulk, Str: []byte("foo")},
			}},
			10,
		},
		{
			"inline with lf only and extra spaces",
			"ECHO   hi\n",
			Value{Kind: KindArray, Array: []Value{
				{Kind: KindBulk, Str: []byte("ECHO")},
				{Kind: KindBulk, Str: []byte("hi")},
			}},
			10,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, n, err := Parse([]byte(tc.in))
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tc.in, err)
			}
			if n != tc.n {
				t.Errorf("n = %d, want %d", n, tc.n)
			}
			assertValue(t, got, tc.want)
		})
	}
}

func assertValue(t *testing.T, got, want Value) {
	t.Helper()
	if got.Kind != want.Kind || got.Null != want.Null || got.Int != want.Int {
		t.Fatalf("value = %+v, want %+v", got, want)
	}
	if !bytes.Equal(got.Str, want.Str) {
		t.Fatalf("Str = %q, want %q", got.Str, want.Str)
	}
	if len(got.Array) != len(want.Array) {
		t.Fatalf("array length = %d, want %d", len(got.Array), len(want.Array))
	}
	for i := range want.Array {
		assertValue(t, got.Array[i], want.Array[i])
	}
}

// The invariant the event loop depends on: every strict prefix of a valid
// frame must report ErrIncomplete with n == 0, never a partial parse and never
// a protocol error. Without this a slow client would be disconnected.
func TestEveryPrefixIsIncomplete(t *testing.T) {
	frames := []string{
		"+OK\r\n",
		"-ERR boom\r\n",
		":1234\r\n",
		"$5\r\nhello\r\n",
		"$-1\r\n",
		"*2\r\n$4\r\nPING\r\n$3\r\nfoo\r\n",
		"*2\r\n*1\r\n:1\r\n+OK\r\n",
		"PING foo\r\n",
	}
	for _, f := range frames {
		for i := 0; i < len(f); i++ {
			v, n, err := Parse([]byte(f[:i]))
			if !errors.Is(err, ErrIncomplete) {
				t.Fatalf("Parse(%q) err = %v (value %+v), want ErrIncomplete", f[:i], err, v)
			}
			if n != 0 {
				t.Fatalf("Parse(%q) n = %d, want 0", f[:i], n)
			}
		}
		if _, n, err := Parse([]byte(f)); err != nil || n != len(f) {
			t.Fatalf("Parse(%q) = n %d, err %v; want full consume", f, n, err)
		}
	}
}

func TestParseProtocolErrors(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"bad integer", ":abc\r\n"},
		{"bad bulk length", "$abc\r\nx\r\n"},
		{"negative bulk length", "$-2\r\n"},
		{"oversized bulk", "$536870913\r\n"},
		{"negative array length", "*-2\r\n"},
		{"oversized array", "*1048577\r\n"},
		{"lone lf after header", "$1\r\nxx\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, n, err := Parse([]byte(tc.in))
			if err == nil || !IsProtocolError(err) {
				t.Fatalf("Parse(%q) err = %v, want protocol error", tc.in, err)
			}
			if n != 0 {
				t.Errorf("n = %d, want 0", n)
			}
			if !strings.HasPrefix(err.Error(), "Protocol error:") {
				t.Errorf("message = %q, want a Protocol error: prefix", err.Error())
			}
		})
	}
}

// A pipelined write must yield the first command only, with n covering exactly
// that frame, so the loop can advance rbuf and parse the next one.
func TestParseCommandStopsAtFirstFrame(t *testing.T) {
	in := []byte("*2\r\n$4\r\nPING\r\n$3\r\nfoo\r\n*1\r\n$4\r\nPING\r\n")
	first := len("*2\r\n$4\r\nPING\r\n$3\r\nfoo\r\n")

	args, n, err := ParseCommand(in)
	if err != nil {
		t.Fatalf("ParseCommand error: %v", err)
	}
	if n != first {
		t.Fatalf("n = %d, want %d", n, first)
	}
	if len(args) != 2 || string(args[0]) != "PING" || string(args[1]) != "foo" {
		t.Fatalf("args = %q, want [PING foo]", args)
	}

	args, n, err = ParseCommand(in[n:])
	if err != nil || n != len(in)-first || len(args) != 1 || string(args[0]) != "PING" {
		t.Fatalf("second command: args %q, n %d, err %v", args, n, err)
	}
}

func TestParseCommandRejectsNonCommandFrames(t *testing.T) {
	for _, in := range []string{"+OK\r\n", ":1\r\n", "*1\r\n:1\r\n", "*1\r\n$-1\r\n"} {
		if _, _, err := ParseCommand([]byte(in)); !IsProtocolError(err) {
			t.Errorf("ParseCommand(%q) err = %v, want protocol error", in, err)
		}
	}
}

func TestParseCommandEmptyInlineLine(t *testing.T) {
	args, n, err := ParseCommand([]byte("\r\n"))
	if err != nil || args != nil || n != 2 {
		t.Fatalf("args %q, n %d, err %v; want nil args consuming the line", args, n, err)
	}
}

// The parser runs on bytes a stranger controls, so its two hard invariants are
// that it never panics and never claims to have consumed more than it was given.
func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"+OK\r\n", ":1\r\n", "$3\r\nabc\r\n", "*2\r\n$4\r\nPING\r\n$3\r\nfoo\r\n",
		"*-1\r\n", "PING\r\n", "$\r\n", "*x\r\n", "\x00\x01",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, buf []byte) {
		v, n, err := Parse(buf)
		if n < 0 || n > len(buf) {
			t.Fatalf("n = %d out of range for %d bytes", n, len(buf))
		}
		if err != nil && n != 0 {
			t.Fatalf("n = %d with error %v, want 0", n, err)
		}
		if err == nil && v.Kind == 0 {
			t.Fatalf("parsed value has no kind")
		}
	})
}

// Parsing is recursive, so deep nesting must be rejected rather than growing
// the loop's stack until it dies.
func TestParseRejectsDeepNesting(t *testing.T) {
	deep := []byte(strings.Repeat("*1\r\n", MaxDepth+1) + ":1\r\n")
	if _, _, err := Parse(deep); !IsProtocolError(err) {
		t.Fatalf("err = %v, want protocol error", err)
	}
	ok := []byte(strings.Repeat("*1\r\n", MaxDepth-1) + ":1\r\n")
	if _, _, err := Parse(ok); err != nil {
		t.Fatalf("nesting just under the limit: %v", err)
	}
}
