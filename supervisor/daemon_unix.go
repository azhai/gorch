//go:build unix

package supervisor

import (
	"fmt"
	"os"
	"syscall"
)

func AcquireLock(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("failed to open lock file: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return fmt.Errorf("another instance is running")
	}

	// Keep file open to hold the lock; caller must call ReleaseLock later.
	lockFile = f
	return nil
}

func ReleaseLock(path string) {
	if lockFile != nil {
		syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		lockFile.Close()
		lockFile = nil
	}
	os.Remove(path)
}
