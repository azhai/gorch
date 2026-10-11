package supervisor

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"
)

// logDayLayout is the suffix added to a rotated log: <path>.YYYY-MM-DD.
const logDayLayout = "2006-01-02"

// logRotationInterval is how often gorch checks whether a service log has
// rolled over into a new day. Rotation only needs to be timely to the minute,
// so this is deliberately much cheaper than the 5s heartbeat.
const logRotationInterval = time.Minute

// logRotator gives每个 service log a one-file-per-day lifetime.
//
// Rotation is driven by the calendar day, not by the file's modification time:
// a busy service rewrites mtime on every line, so a file that has been written
// to since midnight would look "current" and never rotate. The day each file
// belongs to is therefore tracked here, seeded from the file's own timestamp
// the first time it is seen (which is what makes catching up after a restart
// work) and advanced whenever a rotation happens.
type logRotator struct {
	mu   sync.Mutex
	days map[string]string // log path -> the day its current content belongs to
}

func newLogRotator() *logRotator {
	return &logRotator{days: make(map[string]string)}
}

// rotate splits path into "<path>.<day>" and truncates the original back to
// zero when the day has moved on since this rotator last looked at it.
//
// Copy-then-truncate (rather than rename) is deliberate: gorch hands the log
// file directly to the service, which keeps that file descriptor for its whole
// lifetime — and keeps it even if gorch itself dies. Renaming would leave the
// service writing into the rotated file forever, so the old content has to
// leave through a copy while the original inode stays in place.
//
// Returns the path the file was rotated to, or "" when nothing was done.
func (r *logRotator) rotate(path string, now time.Time) (string, error) {
	if path == "" {
		return "", nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	info, err := os.Stat(path)
	if err != nil {
		// Missing or unreadable: nothing to rotate, and not worth surfacing —
		// the service may simply not have written anything yet.
		delete(r.days, path)
		return "", nil
	}

	// Only regular files rotate. /dev/null, fifos and character devices have no
	// useful history and truncating them would be actively wrong.
	if !info.Mode().IsRegular() {
		delete(r.days, path)
		return "", nil
	}

	today := now.Format(logDayLayout)
	day, seen := r.days[path]
	if !seen {
		// First sighting: trust the file's own timestamp so a file left behind
		// by a previous run is still split by the day it was written on.
		day = info.ModTime().Format(logDayLayout)
		r.days[path] = day
	}
	if today <= day {
		return "", nil
	}

	r.days[path] = today
	if info.Size() == 0 {
		return "", nil
	}

	rotated := fmt.Sprintf("%s.%s", path, day)
	if err := copyFile(path, rotated); err != nil {
		return "", fmt.Errorf("failed to copy %s to %s: %w", path, rotated, err)
	}
	if err := os.Truncate(path, 0); err != nil {
		return "", fmt.Errorf("failed to truncate %s: %w", path, err)
	}
	return rotated, nil
}

// forget drops bookkeeping for a path that is no longer configured.
func (r *logRotator) forget(path string) {
	r.mu.Lock()
	delete(r.days, path)
	r.mu.Unlock()
}

// copyFile duplicates src into dst, replacing dst if it already exists.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// serviceLogPaths returns every log file referenced by the current config.
func (s *Supervisor) serviceLogPaths() map[string]struct{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	paths := make(map[string]struct{})
	for _, svc := range s.cfg.Services {
		if svc.STDOUT != "" {
			paths[svc.STDOUT] = struct{}{}
		}
		if svc.STDERR != "" {
			paths[svc.STDERR] = struct{}{}
		}
	}
	return paths
}

// rotateServiceLogs rotates every service log whose day has rolled over.
// Paths are collected under the supervisor lock, but the copies themselves run
// without it: copying a large log can take a while and must not block the rest
// of the supervisor.
func (s *Supervisor) rotateServiceLogs(now time.Time) {
	paths := s.serviceLogPaths()

	for path := range paths {
		rotated, err := s.logRotator.rotate(path, now)
		if err != nil {
			slog.Warn("failed to rotate log file", "path", path, "error", err)
			continue
		}
		if rotated != "" {
			slog.Info("rotated log file", "path", path, "rotated", rotated)
		}
	}
}

// logRotationLoop rotates logs once at startup (catching up on days gorch was
// not running) and then checks once a minute.
func (s *Supervisor) logRotationLoop(ctx context.Context) {
	s.rotateServiceLogs(time.Now())

	ticker := time.NewTicker(logRotationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.rotateServiceLogs(now)
		}
	}
}
