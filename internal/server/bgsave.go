package server

import "github.com/SVIGHNESH/RedForge/internal/store"

// BackgroundSaver writes a snapshot off the loop thread. internal/snapshot
// implements it; it is declared here so the BGSAVE handler in
// internal/command reaches it through the Server without importing the
// snapshot package, the same way Appender keeps command free of aof.
type BackgroundSaver interface {
	// InProgress reports whether a save is running. Read on the loop.
	InProgress() bool
	// Start hands view, taken at sequence boundary, to the writer
	// goroutine. It runs on the loop thread and must not block. The saver
	// releases the view on the loop thread when the write finishes.
	Start(boundary uint64, view store.SnapshotView) error
}

// SetBackgroundSaver installs the BGSAVE writer. Call it before Serve.
func (s *Server) SetBackgroundSaver(b BackgroundSaver) { s.saver = b }

// BackgroundSaver returns the installed writer, or nil when persistence is
// not configured (unit tests).
func (s *Server) BackgroundSaver() BackgroundSaver { return s.saver }
