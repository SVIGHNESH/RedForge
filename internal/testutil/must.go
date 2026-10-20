package testutil

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/SVIGHNESH/RedForge/internal/resp"
)

// Pipeline writes every command, flushes once, then reads one reply per
// command in order. Ordering is the property CC2 asserts, so replies are
// returned exactly as they arrive.
func (c *Client) Pipeline(cmds [][]string) ([]resp.Value, error) {
	for _, args := range cmds {
		if err := c.write(args); err != nil {
			return nil, err
		}
	}
	if err := c.w.Flush(); err != nil {
		return nil, err
	}
	out := make([]resp.Value, 0, len(cmds))
	for range cmds {
		v, err := c.read()
		if err != nil {
			return out, err
		}
		out = append(out, v)
	}
	return out, nil
}

// Send writes raw protocol bytes without reading anything, for tests that
// pipeline hand-built frames. Read the replies with Read.
func (c *Client) Send(raw []byte) error {
	if _, err := c.w.Write(raw); err != nil {
		return err
	}
	return c.w.Flush()
}

// Read returns the next reply frame.
func (c *Client) Read() (resp.Value, error) { return c.read() }

// MustDo is Do that fails the test on a transport or framing error. RESP
// error replies are returned, not failed on.
func (c *Client) MustDo(t testing.TB, args ...string) resp.Value {
	t.Helper()
	v, err := c.Do(args...)
	if err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return v
}

// MustOK runs a command that must reply +OK.
func (c *Client) MustOK(t testing.TB, args ...string) {
	t.Helper()
	if v := c.MustDo(t, args...); v.Kind != resp.KindSimpleString || string(v.Str) != "OK" {
		t.Fatalf("%v replied %s, want OK", args, Show(v))
	}
}

// MustInt runs a command that must reply with an integer.
func (c *Client) MustInt(t testing.TB, args ...string) int64 {
	t.Helper()
	v := c.MustDo(t, args...)
	if v.Kind != resp.KindInteger {
		t.Fatalf("%v replied %s, want an integer", args, Show(v))
	}
	return v.Int
}

// MustBulk runs a command that must reply with a non-nil bulk string.
func (c *Client) MustBulk(t testing.TB, args ...string) string {
	t.Helper()
	v := c.MustDo(t, args...)
	if v.Kind != resp.KindBulk || v.Null {
		t.Fatalf("%v replied %s, want a bulk string", args, Show(v))
	}
	return string(v.Str)
}

// MustNil runs a command that must reply with the null bulk string.
func (c *Client) MustNil(t testing.TB, args ...string) {
	t.Helper()
	if v := c.MustDo(t, args...); v.Kind != resp.KindBulk || !v.Null {
		t.Fatalf("%v replied %s, want nil", args, Show(v))
	}
}

// MustError runs a command that must reply with an error starting with
// prefix, and returns the full error text.
func (c *Client) MustError(t testing.TB, prefix string, args ...string) string {
	t.Helper()
	v := c.MustDo(t, args...)
	if v.Kind != resp.KindError || !strings.HasPrefix(string(v.Str), prefix) {
		t.Fatalf("%v replied %s, want an error starting %q", args, Show(v), prefix)
	}
	return string(v.Str)
}

// MustStrings runs a command that must reply with an array of non-nil bulk
// strings.
func (c *Client) MustStrings(t testing.TB, args ...string) []string {
	t.Helper()
	v := c.MustDo(t, args...)
	if v.Kind != resp.KindArray || v.Null {
		t.Fatalf("%v replied %s, want an array", args, Show(v))
	}
	out := make([]string, len(v.Array))
	for i, e := range v.Array {
		if e.Kind != resp.KindBulk || e.Null {
			t.Fatalf("%v element %d is %s, want a bulk string", args, i, Show(e))
		}
		out[i] = string(e.Str)
	}
	return out
}

// CLI runs one command through the real redis-cli against port and returns
// its output with the trailing newline removed. Extra redis-cli options such
// as --no-raw go before the command words. The test is skipped where
// redis-cli is not installed; CI installs redis-tools, so nothing skips there.
func CLI(t testing.TB, port int, args ...string) string {
	t.Helper()
	path := RedisCLI(t)
	argv := append([]string{"-h", "127.0.0.1", "-p", strconv.Itoa(port)}, args...)
	out, err := exec.Command(path, argv...).CombinedOutput()
	if err != nil {
		t.Fatalf("redis-cli %v: %v\n%s", args, err, out)
	}
	return strings.TrimSuffix(string(out), "\n")
}

// RedisCLI returns the redis-cli path, skipping the test when it is missing.
func RedisCLI(t testing.TB) string {
	t.Helper()
	path, err := exec.LookPath("redis-cli")
	if err != nil {
		t.Skip("redis-cli not installed (CI installs redis-tools)")
	}
	return path
}
