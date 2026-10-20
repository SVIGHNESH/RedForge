package server

import (
	"encoding/binary"
	"errors"
	"fmt"
	"syscall"
)

// RegisterFd asks the loop to watch fd for readability and run onReadable on
// the loop thread when it is. Background goroutines use it to hand results
// back to the loop without sharing state with it (MASTER-PLAN 3.2): the BGSAVE
// writer signals an eventfd registered here. Call it before Serve; a call
// before Listen is applied when Listen creates the epoll instance.
//
// The descriptor is level-triggered, so onReadable must drain it or it runs
// again on the next wait.
func (s *Server) RegisterFd(fd int, onReadable func()) error {
	if s.fdHandlers == nil {
		s.fdHandlers = make(map[int]func())
	}
	if _, dup := s.fdHandlers[fd]; dup {
		return fmt.Errorf("server: fd %d already registered", fd)
	}
	s.fdHandlers[fd] = onReadable
	if s.epfd == 0 {
		return nil
	}
	return s.addFd(fd)
}

func (s *Server) addFd(fd int) error {
	ev := syscall.EpollEvent{Events: syscall.EPOLLIN, Fd: int32(fd)}
	if err := syscall.EpollCtl(s.epfd, syscall.EPOLL_CTL_ADD, fd, &ev); err != nil {
		return fmt.Errorf("server: epoll_ctl add fd %d: %w", fd, err)
	}
	return nil
}

// Notifier is a Linux eventfd: a counter in the kernel that any goroutine can
// bump with Signal and that the loop sees as a readable descriptor in epoll.
// It carries no data; whatever the signalling goroutine produced travels
// through a channel or a field that the loop reads after Drain.
type Notifier struct {
	fd int
}

// NewNotifier creates a non-blocking, close-on-exec eventfd. The syscall
// package has no wrapper for eventfd2, so it is invoked directly; x/sys is
// not allowed (standard library only).
func NewNotifier() (*Notifier, error) {
	const efdNonblock, efdCloexec = syscall.O_NONBLOCK, syscall.O_CLOEXEC
	r1, _, errno := syscall.Syscall(syscall.SYS_EVENTFD2, 0, efdNonblock|efdCloexec, 0)
	if errno != 0 {
		return nil, fmt.Errorf("server: eventfd2: %w", errno)
	}
	return &Notifier{fd: int(r1)}, nil
}

// FD is the descriptor to pass to RegisterFd.
func (n *Notifier) FD() int { return n.fd }

// Signal adds one to the counter, making the descriptor readable. It is safe
// to call from any goroutine.
func (n *Notifier) Signal() error {
	var b [8]byte
	binary.NativeEndian.PutUint64(b[:], 1)
	for {
		_, err := syscall.Write(n.fd, b[:])
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		return err
	}
}

// Drain resets the counter so the descriptor is no longer readable. Call it
// on the loop thread at the start of the RegisterFd callback.
func (n *Notifier) Drain() {
	var b [8]byte
	for {
		_, err := syscall.Read(n.fd, b[:])
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		return
	}
}

// Close releases the descriptor.
func (n *Notifier) Close() error { return syscall.Close(n.fd) }
