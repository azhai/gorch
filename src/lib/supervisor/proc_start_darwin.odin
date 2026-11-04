#+build darwin
package supervisor

import "core:sys/darwin"
import "core:sys/posix"

// proc_start_time reports when `pid` started, in unix seconds. It asks the kernel
// directly: `ps -o etime=` would spawn a helper we may not be allowed to run, and
// its second-resolution output would need parsing. proc_pidinfo is the same data
// ps reads, and it is also how pid reuse is detected during recovery.
proc_start_time :: proc(pid: int) -> (i64, bool) {
	if pid <= 0 {
		return 0, false
	}
	info: darwin.proc_bsdinfo
	want := i32(size_of(info))
	if darwin.proc_pidinfo(posix.pid_t(pid), .BSDINFO, 0, &info, want) != want {
		return 0, false
	}
	if info.pbi_start_tvsec == 0 {
		return 0, false
	}
	return i64(info.pbi_start_tvsec), true
}
