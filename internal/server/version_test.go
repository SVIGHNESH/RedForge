package server

import "testing"

// The version string is an invariant of the INFO contract: redis-cli reads
// redis_version, and the acceptance criteria of every task pin this exact form.
func TestVersionIsPinned(t *testing.T) {
	if Version != "0.1.0-rfs" {
		t.Fatalf("Version = %q, want %q", Version, "0.1.0-rfs")
	}
}
