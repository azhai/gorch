package logfile

// One log file per service per day.
//
// Rotation is driven by the calendar day rather than by mtime: a busy service
// touches mtime on every line, so a file written to since midnight would look
// current forever. The day a file belongs to is tracked per path here, seeded
// from the file's own timestamp the first time it is seen (which is what lets
// gorch catch up on days it was not running).
//
// The split is copy-then-truncate, never rename: the service holds the original
// descriptor for its whole life and would keep writing into the rotated file
// after a rename. Truncating in place keeps that descriptor valid.

import "core:fmt"
import "core:os"
import "core:sync"
import "core:time"

// A Day is YYYYMMDD packed into an integer: cheap to store and ordered the way
// a calendar is, so "has the day moved on" is a single comparison.
Day :: distinct u32

pack_day :: proc(year, month, day: int) -> Day {
	return Day(u32(year) * 10000 + u32(month) * 100 + u32(day))
}

Rotator :: struct {
	mu:   sync.Mutex,
	days: map[string]Day, // log path -> the day its current content belongs to
}

rotator_init :: proc(r: ^Rotator) {
	r.days = make(map[string]Day)
}

rotator_destroy :: proc(r: ^Rotator) {
	delete(r.days)
	r.days = nil
}

// rotate splits `path` into "<path>.<day>" and truncates the original when the
// day has moved on since the rotator last looked at it. It returns the path the
// content was moved to, or "" when nothing was done.
rotate :: proc(r: ^Rotator, path: string, now: time.Time) -> (rotated: string, err: string) {
	if len(path) == 0 {
		return "", ""
	}

	info, serr := os.stat(path, context.temp_allocator)
	if serr != nil {
		// Missing or unreadable: nothing to rotate. Forgetting the day means a
		// file that reappears is seeded from its own timestamp again.
		forget(r, path)
		return "", ""
	}
	// Only regular files rotate. /dev/null, fifos and character devices have no
	// useful history and truncating them would be actively wrong.
	if info.type != .Regular {
		forget(r, path)
		return "", ""
	}

	year, month, day := day_of(now)
	today := pack_day(year, month, day)

	sync.mutex_lock(&r.mu)
	seen_day, seen := r.days[path]
	if !seen {
		// First sighting: trust the file's own timestamp, so a log left behind
		// by a previous run is still filed under the day it was written on.
		sy, sm, sd := day_of(info.modification_time)
		seen_day = pack_day(sy, sm, sd)
		r.days[path] = seen_day
	}
	if today <= seen_day {
		sync.mutex_unlock(&r.mu)
		return "", ""
	}
	sync.mutex_unlock(&r.mu)

	if info.size == 0 {
		remember(r, path, today)
		return "", ""
	}

	dst := fmt.aprintf("{}.{:04d}-{:02d}-{:02d}", path, year, month, day)
	if cerr := copy_file(path, dst); len(cerr) > 0 {
		// Leave the recorded day alone so the next pass retries instead of
		// silently losing the old content.
		delete(dst)
		return "", cerr
	}

	f, oerr := os.open(path, {.Write})
	if oerr != nil {
		delete(dst)
		return "", fmt.aprintf("cannot open {} to truncate: {}", path, oerr)
	}
	terr := os.truncate(f, 0)
	os.close(f)
	if terr != nil {
		delete(dst)
		return "", fmt.aprintf("cannot truncate {}: {}", path, terr)
	}

	remember(r, path, today)
	return dst, ""
}

@(private = "file")
remember :: proc(r: ^Rotator, path: string, day: Day) {
	sync.mutex_lock(&r.mu)
	r.days[path] = day
	sync.mutex_unlock(&r.mu)
}
// forget drops the bookkeeping for a path that is no longer configured.
forget :: proc(r: ^Rotator, path: string) {
	sync.mutex_lock(&r.mu)
	delete_key(&r.days, path)
	sync.mutex_unlock(&r.mu)
}

@(private = "file")
copy_file :: proc(src, dst: string) -> string {
	src_file, ierr := os.open(src, {.Read})
	if ierr != nil {
		return fmt.aprintf("cannot open {}: {}", src, ierr)
	}
	defer os.close(src_file)

	dst_file, oerr := os.open(dst, {.Write, .Create, .Trunc})
	if oerr != nil {
		return fmt.aprintf("cannot create {}: {}", dst, oerr)
	}
	defer os.close(dst_file)

	buf: [64 * 1024]byte
	for {
		n, rerr := os.read(src_file, buf[:])
		if n > 0 {
			if _, werr := os.write(dst_file, buf[:n]); werr != nil {
				return fmt.aprintf("cannot write {}: {}", dst, werr)
			}
		}
		if rerr != nil || n == 0 {
			break
		}
	}
	return ""
}
