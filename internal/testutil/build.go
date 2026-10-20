package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

var (
	buildOnce sync.Once
	buildDir  string
	buildPath string
	buildErr  error
	buildOut  []byte
)

// RepoRoot is the repository root, found from this file's location so tests
// work from any package directory.
func RepoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// BuildOnce compiles cmd/redis-from-scratch once per test process and returns
// the binary path. Every integration, crash and expiry test runs this binary,
// so they exercise exactly what ships. Call RemoveBuild from TestMain to
// delete it when the package's tests finish.
func BuildOnce(t testing.TB) string {
	t.Helper()
	buildOnce.Do(func() {
		buildDir, buildErr = os.MkdirTemp("", "rfs-bin-")
		if buildErr != nil {
			return
		}
		buildPath = filepath.Join(buildDir, "redis-from-scratch")
		cmd := exec.Command("go", "build", "-o", buildPath, "./cmd/redis-from-scratch")
		cmd.Dir = RepoRoot()
		buildOut, buildErr = cmd.CombinedOutput()
	})
	if buildErr != nil {
		t.Fatalf("testutil: building the server failed: %v\n%s", buildErr, buildOut)
	}
	return buildPath
}

// RemoveBuild deletes the binary BuildOnce produced, if any.
func RemoveBuild() {
	if buildDir != "" {
		os.RemoveAll(buildDir)
	}
}
