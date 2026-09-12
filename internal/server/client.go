package server

// Client is one accepted connection. It is owned entirely by the event loop
// thread: nothing else in the process may touch these fields, which is what
// removes the need for a lock anywhere in the hot path.
type Client struct {
	fd   int
	rbuf []byte
	wbuf []byte

	// closing marks a client that must be closed once wbuf has drained, set
	// by QUIT and by protocol errors.
	closing bool
	// wantWrite records whether the fd is currently registered for EPOLLOUT,
	// so the loop does not issue a redundant EpollCtl on every flush.
	wantWrite bool
}

// FD returns the client's file descriptor. Handlers use it only for logging
// and for INFO.
func (c *Client) FD() int { return c.fd }

// Close marks the client to be closed once its pending output has been
// written. Handlers call this instead of closing the socket themselves,
// because the reply to QUIT still has to reach the client.
func (c *Client) Close() { c.closing = true }

// Closing reports whether the client is waiting to be closed once its output
// has drained.
func (c *Client) Closing() bool { return c.closing }
