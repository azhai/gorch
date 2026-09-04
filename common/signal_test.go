package common

import (
	"os"
	"syscall"
	"testing"
	"time"
)

// TestSetupSignalHandler verifies the handler is invoked for SIGHUP — the
// signal gorch uses to reload its configuration.
func TestSetupSignalHandler(t *testing.T) {
	got := make(chan os.Signal, 4)
	SetupSignalHandler(func(sig os.Signal) { got <- sig })

	// signal.Notify is registered before SetupSignalHandler returns, so sending
	// the signal here is safe: it will be delivered to our channel rather than
	// triggering the default terminate behaviour.
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("kill: %v", err)
	}

	select {
	case sig := <-got:
		if sig != syscall.SIGHUP {
			t.Errorf("handler received %v, want SIGHUP", sig)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handler was never called for SIGHUP")
	}
}
