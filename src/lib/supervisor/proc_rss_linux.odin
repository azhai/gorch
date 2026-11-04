#+build linux
package supervisor

import "core:fmt"
import "core:os"
import "core:strconv"
import "core:strings"
import "core:sys/posix"

// rss_sample walks /proc once. Each numeric entry contributes its parent pid
// (stat field 4) and its resident pages (stat field 24), which are scaled by
// the page size from sysconf: aarch64 kernels may run page sizes other than
// 4K. Processes that exit mid-walk are simply absent from the maps.
rss_sample :: proc() -> (parents: map[int]int, rss: map[int]f64, ok: bool) {
	entries, err := os.read_all_directory_by_path("/proc", context.allocator)
	if err != nil {
		return
	}
	defer delete(entries)

	page := f64(posix.sysconf(posix._SC_PAGE_SIZE))
	if page <= 0 {
		page = 4096
	}

	parents = make(map[int]int)
	rss = make(map[int]f64)
	for e in entries {
		pid64, ok := strconv.parse_i64(e.name)
		if !ok || pid64 <= 0 {
			continue
		}
		data, ferr := os.read_entire_file_from_path(
			fmt.tprintf("/proc/{}/stat", e.name),
			context.allocator,
		)
		if ferr != nil {
			continue
		}
		// The command name is wrapped in parentheses and may itself contain
		// spaces or parentheses, so fields are counted from the closing ')',
		// never from the start: fields[0] is stat field 3.
		stat := string(data)
		close_paren := strings.last_index_byte(stat, ')')
		if close_paren < 0 {
			delete(data)
			continue
		}
		fields := strings.fields(stat[close_paren + 1:])
		if len(fields) < 22 {
			delete(data)
			continue
		}
		pp, pok := strconv.parse_i64(fields[1])  // stat field 4: ppid
		rp, rok := strconv.parse_i64(fields[21]) // stat field 24: rss, pages
		delete(data)
		if !pok || !rok || pp <= 0 {
			continue
		}
		parents[int(pid64)] = int(pp)
		rss[int(pid64)] = f64(rp) * page / (1024 * 1024)
	}
	return parents, rss, true
}
