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

// Each expected message was captured from a live redis-server (Valkey 9.1,
// same wording as Redis 7) fed the same bytes, so a client sees identical
// errors from both servers.
func TestParseCommandProtocolErrorsMatchRedis(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"null bulk argument", "*2\r\n$4\r\nECHO\r\n$-1\r\n", "Protocol error: invalid bulk length"},
		{"plus in bulk length", "*1\r\n$+4\r\nPING\r\n", "Protocol error: invalid bulk length"},
		{"letter in bulk length", "*1\r\n$x\r\n", "Protocol error: invalid bulk length"},
		{"leading zero in bulk length", "*1\r\n$04\r\nPING\r\n", "Protocol error: invalid bulk length"},
		{"oversized bulk length", "*1\r\n$536870913\r\n", "Protocol error: invalid bulk length"},
		{"plus in array length", "*+1\r\n$4\r\nPING\r\n", "Protocol error: invalid multibulk length"},
		{"array above the cap", "*1048577\r\n", "Protocol error: invalid multibulk length"},
		{"integer element", "*1\r\n:1\r\n", "Protocol error: expected '$', got ':'"},
		{"nested array element", "*1\r\n*1\r\n$4\r\nPING\r\n", "Protocol error: expected '$', got '*'"},
		{"control byte element", "*1\r\n\n", `Protocol error: expected '$', got '\xa'`},
		{"lone lf after array header", "*1\n", "Protocol error: unbalanced line ending"},
		{"lone lf after bulk", "*1\r\n$4\r\nPING\n\n", "Protocol error: unbalanced line ending"},
		{"inline over 64 KB", strings.Repeat("a", MaxInlineLength+1), "Protocol error: too big inline request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args, n, err := ParseCommand([]byte(tc.in))
			if !IsProtocolError(err) {
				t.Fatalf("ParseCommand(%q) = %q, %d, %v; want protocol error", tc.in, args, n, err)
			}
			if err.Error() != tc.want {
				t.Errorf("message = %q, want %q", err.Error(), tc.want)
			}
		})
	}
}

// Redis frames anything that does not start with '*' as an inline command, so
// a bare "+OK" is the command "+OK" (later an unknown-command error), not a
// protocol error. Empty requests are consumed and produce no command.
func TestParseCommandFramingMatchesRedis(t *testing.T) {
	cases := []struct {
		name string
		in   string
		args []string
		n    int
	}{
		{"bare simple string is inline", "+OK\r\n", []string{"+OK"}, 5},
		{"bare integer is inline", ":1\r\n", []string{":1"}, 4},
		{"bare bulk header is inline", "$4\r\nPING\r\n", []string{"$4"}, 4},
		{"empty array is ignored", "*0\r\nPING\r\n", nil, 4},
		{"null array is ignored", "*-1\r\nPING\r\n", nil, 5},
		{"empty bulk argument", "*2\r\n$4\r\nECHO\r\n$0\r\n\r\n", []string{"ECHO", ""}, 20},
		{"inline with lf only", "PING\n", []string{"PING"}, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args, n, err := ParseCommand([]byte(tc.in))
			if err != nil {
				t.Fatalf("ParseCommand(%q) err = %v", tc.in, err)
			}
			if n != tc.n {
				t.Errorf("n = %d, want %d", n, tc.n)
			}
			if len(args) != len(tc.args) {
				t.Fatalf("args = %q, want %q", args, tc.args)
			}
			for i := range args {
				if string(args[i]) != tc.args[i] {
					t.Errorf("args[%d] = %q, want %q", i, args[i], tc.args[i])
				}
			}
		})
	}
}

// Every proper prefix of a valid command must be reported incomplete, never an
// error, or a client whose bytes arrive one at a time would be cut off.
func TestParseCommandEveryPrefixIsIncomplete(t *testing.T) {
	full := "*3\r\n$3\r\nSET\r\n$1\r\nk\r\n$10\r\n0123456789\r\n"
	for i := 0; i < len(full); i++ {
		if _, n, err := ParseCommand([]byte(full[:i])); err != ErrIncomplete || n != 0 {
			t.Fatalf("prefix %q: n %d, err %v; want ErrIncomplete", full[:i], n, err)
		}
	}
	if _, n, err := ParseCommand([]byte(full)); err != nil || n != len(full) {
		t.Fatalf("full frame: n %d, err %v", n, err)
	}
}

// An error already visible in the received bytes is reported before the rest
// of the frame arrives, so a client cannot hold a bad frame open.
func TestParseCommandReportsErrorsBeforeTheFrameCompletes(t *testing.T) {
	for _, in := range []string{"*2\r\n:", "*2\r\n$-1\r\n", "*2\r\n$4\r\nPING\r\n*"} {
		if _, _, err := ParseCommand([]byte(in)); !IsProtocolError(err) {
			t.Errorf("ParseCommand(%q) err = %v, want protocol error", in, err)
		}
	}
}

func TestParseIntMatchesRedisString2ll(t *testing.T) {
	good := map[string]int64{"0": 0, "7": 7, "-1": -1, "1048576": 1048576, "9223372036854775807": 1<<63 - 1}
	for in, want := range good {
		if got, err := parseInt([]byte(in)); err != nil || got != want {
			t.Errorf("parseInt(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "-", "+1", "01", "-0", " 1", "1 ", "1a", "9223372036854775808", "12345678901234567890"} {
		if _, err := parseInt([]byte(in)); err == nil {
			t.Errorf("parseInt(%q) accepted, want error", in)
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
		// T1.01 malformed cases.
		"*2\r\n$4\r\nECHO\r\n$-1\r\n", "*1\r\n$+4\r\nPING\r\n", "*+1\r\n$4\r\nPING\r\n",
		"*1\r\n$04\r\nPING\r\n", "*1048577\r\n", "*1\r\n:1\r\n", "*1\r\n*1\r\n$4\r\nPING\r\n",
		"*1\n", "*1\r\n$4\r\nPING\n\n", "*0\r\n", "$4\r\nPING\r\n", "HELLO 3\r\n",
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

// ParseCommand is the path the loop actually runs on client bytes. Besides the
// FuzzParse invariants, an error must never be followed by args, and a
// successful parse of a complete command must be stable when more bytes are
// appended (pipelining depends on it).
func FuzzParseCommand(f *testing.F) {
	for _, seed := range []string{
		"PING\r\n", "*1\r\n$4\r\nPING\r\n", "*2\r\n$4\r\nECHO\r\n$-1\r\n", "*1\r\n$+4\r\nPING\r\n",
		"*+1\r\n$4\r\nPING\r\n", "*1048577\r\n", "*1\r\n:1\r\n", "*1\n", "*0\r\n", "*-1\r\n",
		"$4\r\nPING\r\n", "+OK\r\n", "HELLO 3\r\n", "\r\n", "*1\r\n$4\r\nPING\n\n",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, buf []byte) {
		args, n, err := ParseCommand(buf)
		if n < 0 || n > len(buf) {
			t.Fatalf("n = %d out of range for %d bytes", n, len(buf))
		}
		if err != nil && (n != 0 || args != nil) {
			t.Fatalf("error %v with n %d args %q", err, n, args)
		}
		if err == nil && n == 0 {
			t.Fatalf("success consumed nothing")
		}
		if err != nil {
			return
		}
		more := append(append([]byte{}, buf...), "*1\r\n$4\r\nPING\r\n"...)
		args2, n2, err2 := ParseCommand(more)
		if err2 != nil || n2 != n || len(args2) != len(args) {
			t.Fatalf("appending bytes changed the first command: %q/%d/%v vs %q/%d", args2, n2, err2, args, n)
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
