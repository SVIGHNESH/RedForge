package server

// Stats holds the counters that command handlers update and INFO reads. Both
// happen on the loop thread, so plain fields are enough; nothing else may
// touch them while Serve runs.
type Stats struct {
	// KeyspaceHits and KeyspaceMisses count lookups that did or did not
	// find a value, for the INFO Stats section.
	KeyspaceHits   int64
	KeyspaceMisses int64

	// KeysLastIterUs is how long the last KEYS spent walking the keyspace,
	// and KeysLastReplyBytes how large its reply was. Together they let the
	// evaluation separate iteration cost from serialisation cost in the
	// KEYS stall measurement (MASTER-PLAN Section 9).
	KeysLastIterUs     int64
	KeysLastReplyBytes int64
}
