#+build darwin
package supervisor

import "core:sys/darwin"
import "core:sys/posix"

// rss_sample asks libproc for the full pid list, takes each process's parent
// from proc_bsdinfo, and fills RSS from proc_pid_rusage. Both calls fail
// harmlessly for kernel internals and processes that exited mid-walk: they
// just do not appear in the maps.
rss_sample :: proc() -> (parents: map[int]int, rss: map[int]f64, ok: bool) {
	buf: [8192]i32
	n := darwin.proc_listallpids(raw_data(&buf), i32(size_of(buf)))
	if n <= 0 {
		return
	}
	if n > i32(len(buf)) {
		n = i32(len(buf))
	}

	parents = make(map[int]int)
	rss = make(map[int]f64)
	for i in 0 ..< int(n) {
		pid := int(buf[i])
		if pid <= 0 {
			continue
		}
		info: darwin.proc_bsdinfo
		want := i32(size_of(info))
		if darwin.proc_pidinfo(posix.pid_t(pid), .BSDINFO, 0, &info, want) != want {
			continue
		}
		parents[pid] = int(info.pbi_ppid)
		ru: darwin.rusage_info_v0
		if darwin.proc_pid_rusage(posix.pid_t(pid), .V0, &ru) == 0 {
			rss[pid] = f64(ru.ri_resident_size) / (1024 * 1024)
		}
	}
	return parents, rss, true
}
