package main

// The machine's UTC offset, for the log rotator.
//
// This lives in the main package on purpose. Calling libc from a library package
// (src/lib/*) is miscompiled by the current toolchain at -o:minimal: the call
// jumps to a stub at the Mach-O header and the process dies before the caller's
// first statement. The identical call from package main links correctly, so the
// zone is read here once and handed to logfile.

import "core:time"

import "../../lib/logfile"

foreign import libc "system:c"

@(private = "file")
C_Tm :: struct {
	sec, min, hour, mday, mon, year: i32, // mon is 0-11, year is years since 1900
	wday, yday, isdst:               i32,
	gmtoff:                          i64, // seconds east of UTC
	zone:                            cstring,
}

@(default_calling_convention = "c")
foreign libc {
	localtime_r :: proc(t: ^i64, result: ^C_Tm) -> ^C_Tm ---
}

// install_utc_offset tells the rotator how far local time runs ahead of UTC, so
// a daily log rolls over at local midnight rather than at 08:00 for a UTC+8
// operator. Failure is not fatal: the rotator then treats days as UTC.
install_utc_offset :: proc() {
	secs := time.time_to_unix(time.now())
	tm: C_Tm
	if localtime_r(&secs, &tm) == nil {
		return
	}
	logfile.set_utc_offset(tm.gmtoff)
}
