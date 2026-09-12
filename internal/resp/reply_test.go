package resp

import (
	"bytes"
	"testing"
)

func TestReplyBytes(t *testing.T) {
	cases := []struct {
		name  string
		reply Reply
		want  string
	}{
		{"simple string", SimpleString("OK"), "+OK\r\n"},
		{"ok singleton", OK, "+OK\r\n"},
		{"pong singleton", PONG, "+PONG\r\n"},
		{"error", Error("ERR unknown command 'foo'"), "-ERR unknown command 'foo'\r\n"},
		{"integer", Integer(42), ":42\r\n"},
		{"negative integer", Integer(-1), ":-1\r\n"},
		{"bulk", Bulk([]byte("hello")), "$5\r\nhello\r\n"},
		{"bulk string", BulkString("hi"), "$2\r\nhi\r\n"},
		{"empty bulk", Bulk([]byte{}), "$0\r\n\r\n"},
		{"null bulk", NullBulk(), "$-1\r\n"},
		{"nil singleton", Nil, "$-1\r\n"},
		{"null array", NullArray(), "*-1\r\n"},
		{"empty array", EmptyArray(), "*0\r\n"},
		{"array", Array(Integer(1), BulkString("two")), "*2\r\n:1\r\n$3\r\ntwo\r\n"},
		{"nested array", Array(Array(SimpleString("a")), NullBulk()), "*2\r\n*1\r\n+a\r\n$-1\r\n"},
		{"array of bulks", ArrayOfBulks([][]byte{[]byte("a"), []byte("bb")}), "*2\r\n$1\r\na\r\n$2\r\nbb\r\n"},
		{"empty array of bulks", ArrayOfBulks(nil), "*0\r\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(tc.reply.AppendTo(nil)); got != tc.want {
				t.Fatalf("AppendTo = %q, want %q", got, tc.want)
			}
		})
	}
}

// The loop appends every reply of an event into one buffer and flushes once,
// so AppendTo must extend what it is given rather than replace it.
func TestAppendToPreservesExistingBytes(t *testing.T) {
	dst := []byte("+FIRST\r\n")
	dst = Integer(7).AppendTo(dst)
	dst = BulkString("x").AppendTo(dst)
	if want := "+FIRST\r\n:7\r\n$1\r\nx\r\n"; string(dst) != want {
		t.Fatalf("buffer = %q, want %q", dst, want)
	}
}

// Every reply this package produces must parse back to the same value, which
// is the property that keeps the writer and the reader from drifting apart.
func TestReplyRoundTripsThroughParse(t *testing.T) {
	cases := []struct {
		reply Reply
		check func(t *testing.T, v Value)
	}{
		{OK, func(t *testing.T, v Value) {
			if v.Kind != KindSimpleString || string(v.Str) != "OK" {
				t.Fatalf("got %+v", v)
			}
		}},
		{Error("ERR boom"), func(t *testing.T, v Value) {
			if v.Kind != KindError || string(v.Str) != "ERR boom" {
				t.Fatalf("got %+v", v)
			}
		}},
		{Integer(-99), func(t *testing.T, v Value) {
			if v.Kind != KindInteger || v.Int != -99 {
				t.Fatalf("got %+v", v)
			}
		}},
		{Bulk([]byte("a\r\nb")), func(t *testing.T, v Value) {
			if v.Kind != KindBulk || !bytes.Equal(v.Str, []byte("a\r\nb")) {
				t.Fatalf("got %+v", v)
			}
		}},
		{NullBulk(), func(t *testing.T, v Value) {
			if v.Kind != KindBulk || !v.Null {
				t.Fatalf("got %+v", v)
			}
		}},
		{NullArray(), func(t *testing.T, v Value) {
			if v.Kind != KindArray || !v.Null {
				t.Fatalf("got %+v", v)
			}
		}},
		{ArrayOfBulks([][]byte{[]byte("PING"), []byte("foo")}), func(t *testing.T, v Value) {
			if v.Kind != KindArray || len(v.Array) != 2 || string(v.Array[1].Str) != "foo" {
				t.Fatalf("got %+v", v)
			}
		}},
		{Array(Array(Integer(1)), SimpleString("z")), func(t *testing.T, v Value) {
			if v.Kind != KindArray || len(v.Array) != 2 || v.Array[0].Array[0].Int != 1 {
				t.Fatalf("got %+v", v)
			}
		}},
	}
	for _, tc := range cases {
		encoded := tc.reply.AppendTo(nil)
		v, n, err := Parse(encoded)
		if err != nil {
			t.Fatalf("Parse(%q) error: %v", encoded, err)
		}
		if n != len(encoded) {
			t.Fatalf("Parse(%q) consumed %d of %d bytes", encoded, n, len(encoded))
		}
		tc.check(t, v)
	}
}

func BenchmarkAppendBulk(b *testing.B) {
	buf := make([]byte, 0, 64)
	payload := []byte("hello")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf = Bulk(payload).AppendTo(buf[:0])
	}
	_ = buf
}

func BenchmarkAppendInteger(b *testing.B) {
	buf := make([]byte, 0, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf = Integer(int64(i)).AppendTo(buf[:0])
	}
	_ = buf
}

func BenchmarkAppendSimpleString(b *testing.B) {
	buf := make([]byte, 0, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf = OK.AppendTo(buf[:0])
	}
	_ = buf
}
