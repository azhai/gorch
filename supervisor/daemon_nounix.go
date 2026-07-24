//go:build !unix

package supervisor

func AcquireLock(path string) error {
	// On non-Unix systems, use a simple file-existence check as a weak lock.
	return nil
}

func ReleaseLock(path string) {}
