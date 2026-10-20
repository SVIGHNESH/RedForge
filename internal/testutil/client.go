package testutil

import (
	"bufio"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/SVIGHNESH/RedForge/internal/resp"
)

// replyTimeout bounds every read so a hung server fails a test instead of
// hanging it.
const replyTimeout = 10 * time.Second

// Client is a minimal RESP2 client over one connection. It writes commands as
// RESP arrays and reads typed replies with the server's own parser. It is for
// load, ordering and crash tests; redis-cli (see CLI) is for compatibility
// sessions.
type Client struct {
	conn net.Conn
	w    *bufio.Writer
	buf  []byte
	tmp  []byte
}

// Dial connects to addr.
func Dial(addr string) (*Client, error) {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, w: bufio.NewWriter(conn), tmp: make([]byte, 64*1024)}, nil
}

// MustDial is Dial that fails the test on error and closes the connection
// when the test ends.
func MustDial(t testing.TB, addr string) *Client {
	t.Helper()
	c, err := Dial(addr)
	if err != nil {
		t.Fatalf("testutil: dial %s: %v", addr, err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// Close closes the connection. Safe to call twice.
func (c *Client) Close() error { return c.conn.Close() }

// Do sends one command and reads its reply. A RESP error reply is returned
// as a Value of KindError, not as a Go error; the error result is reserved
// for transport and framing failures.
func (c *Client) Do(args ...string) (resp.Value, error) {
	if err := c.write(args); err != nil {
		return resp.Value{}, err
	}
	if err := c.w.Flush(); err != nil {
		return resp.Value{}, err
	}
	return c.read()
}

func (c *Client) write(args []string) error {
	c.w.WriteByte('*')
	c.w.WriteString(strconv.Itoa(len(args)))
	c.w.WriteString("\r\n")
	for _, a := range args {
		c.w.WriteByte('$')
		c.w.WriteString(strconv.Itoa(len(a)))
		c.w.WriteString("\r\n")
		c.w.WriteString(a)
		if _, err := c.w.WriteString("\r\n"); err != nil {
			return err
		}
	}
	return nil
}

// read returns the next reply frame, reading more bytes while the buffer
// holds only part of one.
func (c *Client) read() (resp.Value, error) {
	for {
		if len(c.buf) > 0 {
			v, n, err := resp.Parse(c.buf)
			if err == nil {
				c.buf = c.buf[n:]
				return v, nil
			}
			if !errors.Is(err, resp.ErrIncomplete) {
				return resp.Value{}, err
			}
		}
		c.conn.SetReadDeadline(time.Now().Add(replyTimeout))
		n, err := c.conn.Read(c.tmp)
		if err != nil {
			return resp.Value{}, err
		}
		c.buf = append(c.buf, c.tmp[:n]...)
	}
}

// Info runs INFO [section] and parses the reply strictly.
func (c *Client) Info(t testing.TB, section string) InfoReply {
	t.Helper()
	args := []string{"INFO"}
	if section != "" {
		args = append(args, section)
	}
	v, err := c.Do(args...)
	if err != nil {
		t.Fatalf("testutil: INFO: %v", err)
	}
	if v.Kind != resp.KindBulk || v.Null {
		t.Fatalf("testutil: INFO replied %s, want a bulk string", Show(v))
	}
	info, err := ParseInfo(string(v.Str))
	if err != nil {
		t.Fatalf("testutil: %v", err)
	}
	return info
}

// Show renders a reply for failure messages: nil, a quoted string, an
// integer, an error or an array.
func Show(v resp.Value) string {
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
			out += Show(e)
		}
		return out + "]"
	default:
		return strconv.Quote(string(v.Str))
	}
}
