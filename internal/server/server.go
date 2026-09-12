package server

import (
	"errors"
	"fmt"
	"net"
	"runtime"
	"sync/atomic"
	"syscall"
)

const (
	// listenBacklog matches Redis's default tcp-backlog.
	listenBacklog = 511
	// readChunk is the scratch buffer the loop reads into before appending to
	// a client's rbuf. One buffer is shared by every client because only the
	// loop thread ever uses it.
	readChunk = 16 * 1024
	// maxEvents is how many ready fds one EpollWait call reports.
	maxEvents = 128
	// tickMs is the EpollWait timeout. It is the loop's only timer: active
	// expiry, the everysec fsync request and the BGSAVE completion check all
	// hang off it (MASTER-PLAN Section 3.1).
	tickMs = 100
)

// ExecuteFunc consumes a client's read buffer and appends replies to its write
// buffer, returning the number of bytes it consumed. T0.05 replaces the echo
// default with incremental RESP parsing and T0.06 with the dispatcher.
type ExecuteFunc func(s *Server, c *Client) int

// Server is the single-threaded event loop. Every field is touched only by the
// loop thread once Serve is running, except the counters and the stop flag,
// which are atomic so tests and signal handlers can read or set them.
type Server struct {
	epfd int
	lfd  int

	clients map[int]*Client
	events  []syscall.EpollEvent
	scratch []byte

	// Execute is called after each read with new bytes in c.rbuf.
	Execute ExecuteFunc

	// Tick runs once per EpollWait timeout, on the loop thread.
	Tick func(s *Server)

	stopping atomic.Bool

	connectedClients   atomic.Int64
	totalConnections   atomic.Int64
	totalCommands      atomic.Int64
	rejectedConnection atomic.Int64
}

// New returns a server that is not yet listening. Execute defaults to echoing
// whatever arrives, which is how T0.04 proves the loop works end to end.
func New() *Server {
	return &Server{
		clients: make(map[int]*Client),
		events:  make([]syscall.EpollEvent, maxEvents),
		scratch: make([]byte, readChunk),
		Execute: echoExecute,
	}
}

func echoExecute(_ *Server, c *Client) int {
	n := len(c.rbuf)
	c.wbuf = append(c.wbuf, c.rbuf...)
	return n
}

// Listen creates the listening socket and the epoll instance. It must be
// called before Serve, and may be called from any goroutine.
func (s *Server) Listen(bind string, port int) error {
	ip := net.ParseIP(bind)
	if ip == nil {
		return fmt.Errorf("server: invalid bind address %q", bind)
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return fmt.Errorf("server: bind address %q is not IPv4", bind)
	}
	if port < 0 || port > 65535 {
		return fmt.Errorf("server: invalid port %d", port)
	}

	lfd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM|syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("server: socket: %w", err)
	}
	if err := syscall.SetsockoptInt(lfd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		syscall.Close(lfd)
		return fmt.Errorf("server: SO_REUSEADDR: %w", err)
	}
	sa := &syscall.SockaddrInet4{Port: port}
	copy(sa.Addr[:], ip4)
	if err := syscall.Bind(lfd, sa); err != nil {
		syscall.Close(lfd)
		return fmt.Errorf("server: bind %s:%d: %w", bind, port, err)
	}
	if err := syscall.Listen(lfd, listenBacklog); err != nil {
		syscall.Close(lfd)
		return fmt.Errorf("server: listen: %w", err)
	}

	epfd, err := syscall.EpollCreate1(syscall.EPOLL_CLOEXEC)
	if err != nil {
		syscall.Close(lfd)
		return fmt.Errorf("server: epoll_create1: %w", err)
	}
	ev := syscall.EpollEvent{Events: syscall.EPOLLIN, Fd: int32(lfd)}
	if err := syscall.EpollCtl(epfd, syscall.EPOLL_CTL_ADD, lfd, &ev); err != nil {
		syscall.Close(lfd)
		syscall.Close(epfd)
		return fmt.Errorf("server: epoll_ctl add listener: %w", err)
	}

	s.lfd, s.epfd = lfd, epfd
	return nil
}

// Port returns the port the listener actually bound, which is what a test that
// passed port 0 needs to connect.
func (s *Server) Port() (int, error) {
	sa, err := syscall.Getsockname(s.lfd)
	if err != nil {
		return 0, err
	}
	in4, ok := sa.(*syscall.SockaddrInet4)
	if !ok {
		return 0, errors.New("server: listener is not IPv4")
	}
	return in4.Port, nil
}

// Serve runs the event loop until Stop is called. It locks the goroutine to
// its OS thread for the whole run: the loop owns the keyspace, and migrating
// it between threads would invalidate that ownership argument.
func (s *Server) Serve() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	for !s.stopping.Load() {
		n, err := syscall.EpollWait(s.epfd, s.events, tickMs)
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			return fmt.Errorf("server: epoll_wait: %w", err)
		}
		for i := 0; i < n; i++ {
			ev := s.events[i]
			fd := int(ev.Fd)
			if fd == s.lfd {
				s.acceptReady()
				continue
			}
			c := s.clients[fd]
			if c == nil {
				continue
			}
			if ev.Events&(syscall.EPOLLHUP|syscall.EPOLLERR) != 0 {
				s.closeClient(c)
				continue
			}
			if ev.Events&syscall.EPOLLOUT != 0 {
				if !s.flush(c) {
					continue
				}
			}
			if ev.Events&syscall.EPOLLIN != 0 {
				s.readReady(c)
			}
		}
		if s.Tick != nil {
			s.Tick(s)
		}
	}
	return nil
}

// Stop asks the loop to return. It is safe to call from another goroutine; the
// loop notices within one tick.
func (s *Server) Stop() { s.stopping.Store(true) }

// acceptReady drains the accept queue. Level-triggered epoll would hand the
// listener back on the next wait anyway, but accepting in a loop keeps a burst
// of connections to one wakeup.
func (s *Server) acceptReady() {
	for {
		fd, _, err := syscall.Accept4(s.lfd, syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC)
		if err != nil {
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EINTR) {
				return
			}
			// Per-connection failures (EMFILE, ECONNABORTED) must not kill
			// the loop; the listener stays registered and we try again later.
			s.rejectedConnection.Add(1)
			return
		}
		ev := syscall.EpollEvent{Events: syscall.EPOLLIN, Fd: int32(fd)}
		if err := syscall.EpollCtl(s.epfd, syscall.EPOLL_CTL_ADD, fd, &ev); err != nil {
			syscall.Close(fd)
			s.rejectedConnection.Add(1)
			continue
		}
		s.clients[fd] = &Client{fd: fd}
		s.connectedClients.Add(1)
		s.totalConnections.Add(1)
	}
}

// readReady reads until EAGAIN, hands the buffer to Execute and flushes once.
func (s *Server) readReady(c *Client) {
	for {
		n, err := syscall.Read(c.fd, s.scratch)
		if err != nil {
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
				break
			}
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			s.closeClient(c)
			return
		}
		if n == 0 {
			// Orderly shutdown from the peer.
			s.closeClient(c)
			return
		}
		c.rbuf = append(c.rbuf, s.scratch[:n]...)
		if n < len(s.scratch) {
			break
		}
	}

	if consumed := s.Execute(s, c); consumed > 0 {
		c.rbuf = c.rbuf[consumed:]
		s.compact(c)
	}
	s.flush(c)
}

// compact reclaims the consumed prefix of rbuf once it is worth the copy, so a
// long-lived client's buffer does not creep upwards forever.
func (s *Server) compact(c *Client) {
	if len(c.rbuf) == 0 {
		c.rbuf = c.rbuf[:0]
		return
	}
	if cap(c.rbuf)-len(c.rbuf) > cap(c.rbuf)/2 {
		c.rbuf = append(make([]byte, 0, len(c.rbuf)), c.rbuf...)
	}
}

// flush writes as much of wbuf as the socket accepts. It reports whether the
// client is still open. A short write leaves the tail and registers EPOLLOUT;
// a drained buffer deregisters it again.
func (s *Server) flush(c *Client) bool {
	for len(c.wbuf) > 0 {
		n, err := syscall.Write(c.fd, c.wbuf)
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
				break
			}
			s.closeClient(c)
			return false
		}
		c.wbuf = c.wbuf[n:]
	}
	if len(c.wbuf) == 0 {
		c.wbuf = c.wbuf[:0]
		if c.closing {
			s.closeClient(c)
			return false
		}
		s.setWantWrite(c, false)
		return true
	}
	s.setWantWrite(c, true)
	return true
}

func (s *Server) setWantWrite(c *Client, want bool) {
	if c.wantWrite == want {
		return
	}
	events := uint32(syscall.EPOLLIN)
	if want {
		events |= syscall.EPOLLOUT
	}
	ev := syscall.EpollEvent{Events: events, Fd: int32(c.fd)}
	if err := syscall.EpollCtl(s.epfd, syscall.EPOLL_CTL_MOD, c.fd, &ev); err != nil {
		s.closeClient(c)
		return
	}
	c.wantWrite = want
}

func (s *Server) closeClient(c *Client) {
	if _, live := s.clients[c.fd]; !live {
		return
	}
	syscall.EpollCtl(s.epfd, syscall.EPOLL_CTL_DEL, c.fd, nil)
	syscall.Close(c.fd)
	delete(s.clients, c.fd)
	s.connectedClients.Add(-1)
}

// Close tears down every client and the listener. Call it after Serve returns.
func (s *Server) Close() error {
	for _, c := range s.clients {
		s.closeClient(c)
	}
	if s.lfd != 0 {
		syscall.Close(s.lfd)
		s.lfd = 0
	}
	if s.epfd != 0 {
		syscall.Close(s.epfd)
		s.epfd = 0
	}
	return nil
}

// Counters for INFO (MASTER-PLAN Section 4.10).

// ConnectedClients is the number of live connections.
func (s *Server) ConnectedClients() int64 { return s.connectedClients.Load() }

// TotalConnections is every connection accepted since startup.
func (s *Server) TotalConnections() int64 { return s.totalConnections.Load() }

// TotalCommands is every command dispatched since startup.
func (s *Server) TotalCommands() int64 { return s.totalCommands.Load() }

// AddCommandsProcessed advances the command counter; the dispatcher owns it.
func (s *Server) AddCommandsProcessed(n int64) { s.totalCommands.Add(n) }
