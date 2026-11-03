#+build linux
package supervisor

import "core:fmt"
import "core:os"
import "core:strconv"
import "core:strings"
import "core:time"

// proc_start_time reports when `pid` started, in unix seconds. Linux has no
// proc_pidinfo, so the data comes from /proc/<pid>/stat: field 22 is the start time
// in clock ticks since boot, and USER_HZ is 100 on every Linux. /proc/uptime turns
// that tick count into an age, which is then subtracted from the current time.
proc_start_time :: proc(pid: int) -> (i64, bool) {
	if pid <= 0 {
		return 0, false
	}
	stat_path := fmt.tprintf("/proc/{}/stat", pid)
	data, err := os.read_entire_file_from_path(stat_path, context.allocator)
	if err != nil {
		return 0, false
	}
	defer delete(data)

	// The command name is wrapped in parentheses and may itself contain spaces or
	// parentheses, so fields are counted from the closing ')', never from the start.
	stat := string(data)
	close_paren := strings.last_index_byte(stat, ')')
	if close_paren < 0 {
		return 0, false
	}
	fields := strings.fields(stat[close_paren + 1:])
	// fields[0] is stat field 3 (state), so field 22 (starttime) is fields[19].
	if len(fields) < 20 {
		return 0, false
	}
	ticks, ok := strconv.parse_i64(fields[19])
	if !ok {
		return 0, false
	}

	uptime_raw, uerr := os.read_entire_file_from_path("/proc/uptime", context.allocator)
	if uerr != nil {
		return 0, false
	}
	defer delete(uptime_raw)
	uptime_fields := strings.fields(string(uptime_raw))
	if len(uptime_fields) == 0 {
		return 0, false
	}
	uptime, fok := strconv.parse_f64(uptime_fields[0])
	if !fok {
		return 0, false
	}

	age := i64(uptime) - ticks / 100
	return time.to_unix_seconds(time.now()) - age, true
}
