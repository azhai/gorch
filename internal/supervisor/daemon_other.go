//go:build !linux

package supervisor

import "fmt"

// Daemonize is not supported on non-Linux platforms.
func Daemonize() (int, error) {
	return 0, fmt.Errorf("daemonize is only supported on Linux")
}
