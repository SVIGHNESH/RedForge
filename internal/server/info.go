package server

// InfoField is one key:value line of an INFO section.
type InfoField struct {
	Key   string
	Value string
}

// InfoProvider returns a subsystem's current INFO fields for one section. It
// is called on the loop thread while INFO runs. Providers keep INFO from
// importing the subsystems it reports on: the log writer (T5.x), the
// snapshot writer (T6.x) and replication (S1) each register their own, so
// internal/command never imports internal/aof or internal/snapshot.
//
// Fields come back as an ordered slice rather than a map so INFO output is
// stable from run to run.
type InfoProvider func() []InfoField

// RegisterInfo adds a provider for a section, named as it appears in INFO
// (for example "Persistence"). Call it before Serve. A provider's field
// replaces a default field with the same key, and new keys are appended.
func (s *Server) RegisterInfo(section string, fn InfoProvider) {
	if s.infoProviders == nil {
		s.infoProviders = make(map[string][]InfoProvider)
	}
	s.infoProviders[section] = append(s.infoProviders[section], fn)
}

// InfoProviders returns the providers registered for section, in
// registration order.
func (s *Server) InfoProviders(section string) []InfoProvider {
	return s.infoProviders[section]
}

// TCPPort is the port the listener is bound to, or 0 before Listen.
func (s *Server) TCPPort() int {
	if s.lfd == 0 {
		return 0
	}
	port, err := s.Port()
	if err != nil {
		return 0
	}
	return port
}

// SetStartMs records when the server started, in Unix ms from the same Clock
// the dispatcher uses, so uptime_in_seconds follows a fake clock in tests.
func (s *Server) SetStartMs(ms int64) { s.startMs = ms }

// StartMs is the start time recorded by SetStartMs.
func (s *Server) StartMs() int64 { return s.startMs }
