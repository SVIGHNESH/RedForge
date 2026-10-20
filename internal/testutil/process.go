package testutil

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// readyTimeout is how long WaitReady waits for PING to answer.
const readyTimeout = 5 * time.Second

// ServerProcess is one running redis-from-scratch binary on a free port, with
// its own temporary working directory. The server's default --dir is ./data,
// so its persistence files land in DataDir() and survive Kill and Restart.
type ServerProcess struct {
	Port int
	// Dir is the process's working directory, unique per ServerProcess.
	Dir string

	t     testing.TB
	bin   string
	flags []string
	cmd   *exec.Cmd
	out   *syncBuffer
	done  chan struct{}
}

// Start builds the binary if needed and starts it on a free port in a fresh
// temporary directory with the given extra flags, then waits until it answers
// PING. The process is killed when the test ends, and its output is logged if
// the test failed.
func Start(t testing.TB, flags ...string) *ServerProcess {
	t.Helper()
	p := &ServerProcess{t: t, bin: BuildOnce(t), Dir: t.TempDir(), flags: flags}
	p.Port = FreePort(t)
	t.Cleanup(func() {
		p.Kill()
		if t.Failed() && p.out != nil {
			t.Logf("server on port %d output:\n%s", p.Port, p.out.String())
			saveLog(t, p)
		}
	})
	p.launch(t)
	return p
}

// Addr is host:port for clients.
func (p *ServerProcess) Addr() string { return "127.0.0.1:" + strconv.Itoa(p.Port) }

// DataDir is where the server keeps appendonly.aof and dump.snap: the
// default --dir ./data resolved against the process's working directory.
func (p *ServerProcess) DataDir() string { return filepath.Join(p.Dir, "data") }

// Output is everything the server printed so far, for assertions on logs.
func (p *ServerProcess) Output() string { return p.out.String() }

func (p *ServerProcess) launch(t testing.TB) {
	t.Helper()
	args := append([]string{"--port", strconv.Itoa(p.Port)}, p.flags...)
	cmd := exec.Command(p.bin, args...)
	cmd.Dir = p.Dir
	if p.out == nil {
		p.out = &syncBuffer{}
	}
	fmt.Fprintf(p.out, "--- start %v\n", args)
	cmd.Stdout = p.out
	cmd.Stderr = p.out
	if err := cmd.Start(); err != nil {
		t.Fatalf("testutil: start server: %v", err)
	}
	p.cmd = cmd
	done := make(chan struct{})
	p.done = done
	go func() {
		cmd.Wait()
		close(done)
	}()
	p.WaitReady(t)
}

// WaitReady dials until PING answers PONG, failing the test after 5 s or as
// soon as the process exits.
func (p *ServerProcess) WaitReady(t testing.TB) {
	t.Helper()
	deadline := time.Now().Add(readyTimeout)
	for {
		select {
		case <-p.done:
			t.Fatalf("testutil: server exited before it was ready:\n%s", p.out.String())
		default:
		}
		if c, err := Dial(p.Addr()); err == nil {
			v, err := c.Do("PING")
			c.Close()
			if err == nil && string(v.Str) == "PONG" {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("testutil: server on port %d not ready after %v:\n%s", p.Port, readyTimeout, p.out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Kill sends SIGKILL and waits for the process to exit. It is the crash in
// every crash-recovery test: no shutdown code runs. Safe to call twice.
func (p *ServerProcess) Kill() {
	if p.cmd == nil || p.cmd.Process == nil {
		return
	}
	select {
	case <-p.done:
		return
	default:
	}
	p.cmd.Process.Signal(syscall.SIGKILL)
	<-p.done
}

// Stop sends SIGTERM and falls back to SIGKILL after 2 s.
func (p *ServerProcess) Stop() {
	if p.cmd == nil || p.cmd.Process == nil {
		return
	}
	p.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
		p.Kill()
	}
}

// Restart kills the process and starts it again with the same port, flags
// and directory, then waits until it is ready.
func (p *ServerProcess) Restart(t testing.TB) {
	t.Helper()
	p.Kill()
	p.launch(t)
}

// Exited reports whether the process has exited.
func (p *ServerProcess) Exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// Client dials the server and closes the connection when the test ends.
func (p *ServerProcess) Client(t testing.TB) *Client {
	t.Helper()
	return MustDial(t, p.Addr())
}

// Info runs INFO [section] on a fresh connection and returns its key:value
// fields.
func (p *ServerProcess) Info(t testing.TB, section string) map[string]string {
	t.Helper()
	c := p.Client(t)
	defer c.Close()
	return c.Info(t, section).Values
}

// SkipIfShort skips a slow test under go test -short (make test-short). A
// test is slow when it waits on the wall clock for more than 2 s; make test
// and CI never pass -short, so nothing is skipped there.
func SkipIfShort(t testing.TB, why string) {
	t.Helper()
	if testing.Short() {
		t.Skip("slow test skipped under -short: " + why)
	}
}

// saveLog writes a failed test's server output to $RFS_SERVER_LOG_DIR, which
// CI uploads as an artifact. Without the variable it does nothing.
func saveLog(t testing.TB, p *ServerProcess) {
	dir := os.Getenv("RFS_SERVER_LOG_DIR")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Logf("testutil: server log dir: %v", err)
		return
	}
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	path := filepath.Join(dir, fmt.Sprintf("%s-%d.log", name, p.Port))
	if err := os.WriteFile(path, []byte(p.out.String()), 0o644); err != nil {
		t.Logf("testutil: write server log: %v", err)
	}
}

// FreePort binds :0 on loopback, reads the port and releases it. Another
// process could take it before the server binds; that race is accepted.
func FreePort(t testing.TB) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("testutil: free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// WaitFor polls cond every 10 ms until it returns true, failing the test
// after timeout.
func WaitFor(t testing.TB, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("testutil: timed out after %v waiting for %s", timeout, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// syncBuffer is a bytes.Buffer safe for the process's output copier and the
// test goroutine at once.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
