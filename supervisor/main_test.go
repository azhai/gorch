package supervisor

import (
	"os"
	"testing"
)

// TestMain redirects the PID-file directory to a private temporary directory so
// the suite never touches /tmp/gorch, which is owned by a running gorch
// instance. Sharing it made tests flaky (files appearing/disappearing under
// them) and risked confusing the live supervisor's orphan cleanup.
func TestMain(m *testing.M) {
	// Use /tmp as the base rather than the shared $TMPDIR: Go's t.TempDir()
	// manages directories under $TMPDIR, and transiently removing/recreating
	// our PID directory there raced with writes made by the tests.
	dir, err := os.MkdirTemp("/tmp", "gorch-test-pids")
	if err != nil {
		panic(err)
	}
	ServicePidDir = dir

	code := m.Run()

	_ = os.RemoveAll(dir)
	// Restore the default for any code that inspects it afterwards.
	ServicePidDir = "/tmp/gorch"
	os.Exit(code)
}
