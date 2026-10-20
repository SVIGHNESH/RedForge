package integration

import (
	"errors"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/resp"
)

// respClient is a minimal test client over one connection: it writes a
// command as a RESP array and reads exactly one reply frame. It stands in for
// testutil.Client until the T1.03 harness lands.
type respClient struct {
	t    *testing.T
	conn net.Conn
	buf  []byte
}

func newRESPClient(t *testing.T, addr string) *respClient {
	t.Helper()
	return &respClient{t: t, conn: dial(t, addr)}
}

// do sends one command and returns its reply. A transport or framing error
// fails the test; a RESP error reply is returned as a value of KindError.
func (c *respClient) do(args ...string) resp.Value {
	c.t.Helper()
	cmd := []byte("*" + strconv.Itoa(len(args)) + "\r\n")
	for _, a := range args {
		cmd = append(cmd, '$')
		cmd = strconv.AppendInt(cmd, int64(len(a)), 10)
		cmd = append(cmd, "\r\n"...)
		cmd = append(cmd, a...)
		cmd = append(cmd, "\r\n"...)
	}
	if _, err := c.conn.Write(cmd); err != nil {
		c.t.Fatalf("write %v: %v", args, err)
	}
	c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	chunk := make([]byte, 64*1024)
	for {
		v, n, err := resp.Parse(c.buf)
		if err == nil {
			c.buf = c.buf[n:]
			return v
		}
		if !errors.Is(err, resp.ErrIncomplete) {
			c.t.Fatalf("reply to %v does not parse: %v", args, err)
		}
		m, err := c.conn.Read(chunk)
		if err != nil {
			c.t.Fatalf("read reply to %v: %v", args, err)
		}
		c.buf = append(c.buf, chunk[:m]...)
	}
}

// integer runs a command that must reply with an integer.
func (c *respClient) integer(args ...string) int64 {
	c.t.Helper()
	v := c.do(args...)
	if v.Kind != resp.KindInteger {
		c.t.Fatalf("%v replied %s, want an integer", args, show(v))
	}
	return v.Int
}

// strings runs a command that must reply with an array of bulk strings.
func (c *respClient) strings(args ...string) []string {
	c.t.Helper()
	v := c.do(args...)
	if v.Kind != resp.KindArray || v.Null {
		c.t.Fatalf("%v replied %s, want an array", args, show(v))
	}
	out := make([]string, len(v.Array))
	for i, e := range v.Array {
		if e.Kind != resp.KindBulk || e.Null {
			c.t.Fatalf("%v element %d is %s, want a bulk string", args, i, show(e))
		}
		out[i] = string(e.Str)
	}
	return out
}

// show renders a reply for a failure message: nil, a quoted string, an
// integer, an error or an array, instead of raw byte slices.
func show(v resp.Value) string {
	switch {
	case v.Null:
		return "nil"
	case v.Kind == resp.KindInteger:
		return strconv.FormatInt(v.Int, 10)
	case v.Kind == resp.KindError:
		return "error " + strconv.Quote(string(v.Str))
	case v.Kind == resp.KindArray:
		out := "["
		for i, e := range v.Array {
			if i > 0 {
				out += " "
			}
			out += show(e)
		}
		return out + "]"
	default:
		return strconv.Quote(string(v.Str))
	}
}

// propertySeeds is the seed list for the randomised property tests: three
// fixed seeds by default so CI is reproducible, or the single seed in
// RFS_PROPERTY_SEED to replay a failure.
func propertySeeds(t *testing.T) []int64 {
	t.Helper()
	if s := os.Getenv("RFS_PROPERTY_SEED"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			t.Fatalf("RFS_PROPERTY_SEED=%q is not an integer", s)
		}
		return []int64{n}
	}
	return []int64{1, 20260920, 424242}
}
