package integration

import (
	"os"
	"strconv"
	"testing"

	"github.com/SVIGHNESH/RedForge/internal/testutil"
)

// TestMain removes the server binary testutil.BuildOnce built for this
// package once every test has run.
func TestMain(m *testing.M) {
	code := m.Run()
	testutil.RemoveBuild()
	os.Exit(code)
}

// propertySeeds is the seed list for the randomised property tests: three
// fixed seeds by default so CI is reproducible, or the single seed in
// RFS_PROPERTY_SEED to replay a failure.
func propertySeeds(t *testing.T) []int64 {
	t.Helper()
	if s := os.Getenv("RFS_PROPERTY_SEED"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			t.Fatalf("RFS_PROPERTY_SEED=%q is not an integer", s)
		}
		return []int64{n}
	}
	return []int64{1, 20260920, 424242}
}
