package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/azhai/gorch/config"
)

// writeAgedLog creates a log file whose modification time sits on the given day.
func writeAgedLog(t *testing.T, path, content string, day time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chtimes(path, day, day); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
}

// TestLogRotator_SplitsFileFromAnEarlierDay covers the catch-up path: the file
// was last written before today, so its content moves to a dated file.
func TestLogRotator_SplitsFileFromAnEarlierDay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.out.log")
	yesterday := time.Now().AddDate(0, 0, -1)
	writeAgedLog(t, path, "day one\n", yesterday)

	rotated, err := newLogRotator().rotate(path, time.Now())
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}

	want := path + "." + yesterday.Format(logDayLayout)
	if rotated != want {
		t.Errorf("rotated to %q, want %q", rotated, want)
	}
	if data, err := os.ReadFile(want); err != nil || string(data) != "day one\n" {
		t.Errorf("rotated content = %q (err=%v), want the original content", data, err)
	}

	// The service keeps writing to the original path, so it must still exist and
	// be empty rather than renamed away.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("original log should still exist: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("original log size = %d, want 0 after rotation", info.Size())
	}
}

func TestLogRotator_LeavesTodaysFileAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.out.log")
	writeAgedLog(t, path, "same day\n", time.Now())

	rotated, err := newLogRotator().rotate(path, time.Now())
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if rotated != "" {
		t.Errorf("rotated to %q, want no rotation for a file written today", rotated)
	}
}

// TestLogRotator_DayChangeWithActiveWriter is the regression test for the
// obvious-but-wrong implementation that compares against the file's mtime: a
// service writing every second keeps mtime on "today", so an mtime check would
// never fire. Rotation must follow the calendar day instead.
func TestLogRotator_DayChangeWithActiveWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.out.log")
	now := time.Now()

	rotator := newLogRotator()
	writeAgedLog(t, path, "line 1\n", now)
	if rotated, err := rotator.rotate(path, now); err != nil || rotated != "" {
		t.Fatalf("first look: rotated=%q err=%v, want no rotation", rotated, err)
	}

	// The service keeps writing, so the mtime stays "today" right up to and past
	// the moment the date rolls over.
	writeAgedLog(t, path, "line 1\nline 2\n", now)
	tomorrow := now.AddDate(0, 0, 1)
	rotated, err := rotator.rotate(path, tomorrow)
	if err != nil {
		t.Fatalf("rotate after midnight: %v", err)
	}

	want := path + "." + now.Format(logDayLayout)
	if rotated != want {
		t.Fatalf("rotated to %q, want %q", rotated, want)
	}
	if info, err := os.Stat(path); err != nil || info.Size() != 0 {
		t.Errorf("original log should be truncated, err=%v size=%d", err, info.Size())
	}

	// And it must not rotate twice on the same new day.
	if again, err := rotator.rotate(path, tomorrow.Add(time.Hour)); err != nil || again != "" {
		t.Errorf("second look on the same day: rotated=%q err=%v, want none", again, err)
	}
}

func TestLogRotator_SkipsEmptyMissingAndNonRegular(t *testing.T) {
	dir := t.TempDir()

	empty := filepath.Join(dir, "empty.log")
	writeAgedLog(t, empty, "", time.Now().AddDate(0, 0, -1))
	rotator := newLogRotator()
	if rotated, err := rotator.rotate(empty, time.Now()); err != nil || rotated != "" {
		t.Errorf("empty file: rotated=%q err=%v, want none", rotated, err)
	}
	if rotated, err := rotator.rotate(filepath.Join(dir, "missing.log"), time.Now()); err != nil || rotated != "" {
		t.Errorf("missing file: rotated=%q err=%v, want none", rotated, err)
	}
	if rotated, err := rotator.rotate("", time.Now()); err != nil || rotated != "" {
		t.Errorf("empty path: rotated=%q err=%v, want none", rotated, err)
	}
	// A directory stands in for /dev/null and friends: not a regular file, so it
	// must be left untouched rather than copied or truncated.
	if rotated, err := rotator.rotate(dir, time.Now().AddDate(0, 0, 1)); err != nil || rotated != "" {
		t.Errorf("directory: rotated=%q err=%v, want none", rotated, err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("directory should be untouched: %v", err)
	}
}

func TestRotateServiceLogs_UsesConfiguredPaths(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "svc.out.log")
	errPath := filepath.Join(dir, "svc.err.log")
	yesterday := time.Now().AddDate(0, 0, -1)
	writeAgedLog(t, out, "out\n", yesterday)
	writeAgedLog(t, errPath, "err\n", yesterday)

	sup := NewSupervisor(&config.Config{
		Services: map[string]config.ServiceConfig{
			"svc": {EXEC_CMD: "true", STDOUT: out, STDERR: errPath},
		},
	})

	sup.rotateServiceLogs(time.Now())

	for _, path := range []string{out, errPath} {
		rotated := path + "." + yesterday.Format(logDayLayout)
		if _, err := os.Stat(rotated); err != nil {
			t.Errorf("expected %s to exist: %v", rotated, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.Size() != 0 {
			t.Errorf("%s size = %d, want 0 after rotation", path, info.Size())
		}
	}
}

// TestLogRotationLoop_RotatesOnStartup covers the catch-up path: gorch may be
// started days after the service last wrote, and must still split that file.
func TestLogRotationLoop_RotatesOnStartup(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "svc.out.log")
	written := time.Now().AddDate(0, 0, -3)
	writeAgedLog(t, out, "old\n", written)

	sup := NewSupervisor(&config.Config{
		Services: map[string]config.ServiceConfig{
			"svc": {EXEC_CMD: "true", STDOUT: out},
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sup.logRotationLoop(ctx)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(out); err == nil && info.Size() == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	<-done

	rotated := out + "." + written.Format(logDayLayout)
	if _, err := os.Stat(rotated); err != nil {
		t.Errorf("startup rotation should have produced %s: %v", rotated, err)
	}
}
