package logfile

// Reading and clearing the log files gorch hands to its services.
//
// A service writes into its log file with O_APPEND and keeps that descriptor for
// its whole life - including across a gorch restart, because the child outlives
// the parent. Nothing here may rename or replace the inode; see rotate.odin.

import "core:fmt"
import "core:os"

// Reading the end of a multi-megabyte log must not cost a multi-megabyte read,
// so the tail window starts small and doubles until it holds enough lines.
TAIL_CHUNK :: 32 * 1024
MAX_LINES :: 5000

// Tail is a window over the end of a file. The lines are subslices of `buf`, so
// a 500-line tail is one allocation instead of 501.
Tail :: struct {
	buf:   []byte, // the whole window, kept so the free in tail_destroy is right
	lines: [dynamic]string,
}

tail_destroy :: proc(t: ^Tail) {
	delete(t.buf)
	delete(t.lines)
	t.buf = nil
	t.lines = nil
}

// tail returns the last `max_lines` lines of `path`. A missing file is not an
// error: the service simply has not written anything yet.
tail :: proc(path: string, max_lines: int) -> (Tail, string) {
	want := max_lines
	if want <= 0 {
		want = 500
	}
	if want > MAX_LINES {
		want = MAX_LINES
	}

	f, oerr := os.open(path, {.Read})
	if oerr != nil {
		if os.exists(path) {
			return {}, fmt.aprintf("cannot open {}: {}", path, oerr)
		}
		return {}, ""
	}
	defer os.close(f)

	size, serr := os.file_size(f)
	if serr != nil {
		return {}, fmt.aprintf("cannot stat {}: {}", path, serr)
	}
	if size <= 0 {
		return {}, ""
	}

	window := i64(TAIL_CHUNK)
	buf: []byte
	used := 0
	for {
		if window > size {
			window = size
		}
		buf = make([]byte, int(window))
		n, rerr := os.read_at(f, buf, size-window)
		if rerr != nil {
			delete(buf)
			return {}, fmt.aprintf("cannot read {}: {}", path, rerr)
		}
		used = n
		// Keep growing until the window starts at byte 0 or holds more
		// separators than we need, so the cut lands on a line boundary.
		if size-window == 0 || count_newlines(buf[:n]) > max_lines {
			break
		}
		delete(buf)
		window *= 2
	}

	view := buf[:used]
	// A window that does not reach byte 0 starts mid-line; that fragment is
	// not a line, so drop it rather than showing half a log entry.
	if size-window > 0 {
		nl := index_byte(view, '\n')
		if nl < 0 {
			return Tail{buf = buf}, ""
		}
		view = view[nl+1:]
	}
	// A trailing newline is a terminator, not an empty last line.
	if len(view) > 0 && view[len(view)-1] == '\n' {
		view = view[:len(view)-1]
	}
	if len(view) == 0 {
		return Tail{buf = buf}, ""
	}

	out := Tail{buf = buf}
	skip := count_newlines(view) + 1 - max_lines
	if skip < 0 {
		skip = 0
	}
	idx := 0
	start := 0
	for i in 0 ..= len(view) {
		if i == len(view) || view[i] == '\n' {
			if idx >= skip {
				line := view[start:i]
				// bufio.Scanner in the Go version dropped the \r of a CRLF
				// file; keep that behaviour so the UI does not show stray
				// carriage returns.
				if len(line) > 0 && line[len(line)-1] == '\r' {
					line = line[:len(line)-1]
				}
				append(&out.lines, string(line))
			}
			idx += 1
			start = i + 1
		}
	}
	return out, ""
}

// clear truncates a log file in place. Truncating rather than deleting keeps the
// service's open descriptor pointing at a valid, empty file.
clear :: proc(path: string) -> string {
	if len(path) == 0 {
		return ""
	}
	if !os.exists(path) {
		return ""
	}
	f, err := os.open(path, {.Write})
	if err != nil {
		return fmt.aprintf("cannot open {}: {}", path, err)
	}
	defer os.close(f)
	if terr := os.truncate(f, 0); terr != nil {
		return fmt.aprintf("cannot truncate {}: {}", path, terr)
	}
	return ""
}

@(private = "file")
count_newlines :: proc(b: []byte) -> int {
	n := 0
	for ch in b {
		if ch == '\n' {
			n += 1
		}
	}
	return n
}

@(private = "file")
index_byte :: proc(b: []byte, needle: byte) -> int {
	for ch, i in b {
		if ch == needle {
			return i
		}
	}
	return -1
}
