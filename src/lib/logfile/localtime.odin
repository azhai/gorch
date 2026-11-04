package logfile

// Which calendar day a moment belongs to.
//
// The rotation boundary is local midnight, not UTC: a "daily" log that rolls over
// at 08:00 for a UTC+8 operator is worse than useless. Getting the zone needs
// libc, and a libc call from a *library* package is miscompiled by the current
// toolchain at -o:minimal - the call jumps to a stub at the Mach-O header and the
// process dies before the caller's first statement runs. The same call from the
// main package links correctly, so the offset is resolved once at startup
// (cmd/gorch) and handed in here. Until it arrives, days are UTC, which is at
// least well defined.

import "core:time"

@(private = "file")
offset_secs: i64

// set_utc_offset tells the rotator how far local time runs ahead of UTC.
set_utc_offset :: proc(seconds: i64) {
	offset_secs = seconds
}

// day_of returns the local calendar date `t` falls on.
day_of :: proc(t: time.Time) -> (year, month, day: int) {
	shifted := time.unix(time.time_to_unix(t) + offset_secs, 0)
	y, m, d := time.date(shifted)
	return y, int(m), d
}
