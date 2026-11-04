#+build darwin
package supervisor

import "core:fmt"
import "core:os"
import "core:strconv"
import "core:strings"
import "core:time"

// macOS has no /proc, so the Go build used `lsof` here too. lsof ships with
// the base system, so an absolute path keeps this independent of PATH.
LSOF_PATH :: "/usr/sbin/lsof"

port_owners :: proc(port: int) -> []int {
	r, w, perr := os.pipe()
	if perr != nil {
		return nil
	}
	arg := fmt.aprintf(":%d", port)
	defer delete(arg)
	// -sTCP:LISTEN is essential: without it lsof also lists clients holding
	// an established connection on this port, which must never be killed.
	// -nP skips hostname/port-name lookups, which otherwise make lsof take
	// seconds on a busy machine and outlive its wait timeout.
	p, err := os.process_start(
		os.Process_Desc {
			command = []string{LSOF_PATH, "-nP", "-ti", arg, "-sTCP:LISTEN"},
			stdout  = w,
		},
	)
	os.close(w)
	if err != nil {
		os.close(r)
		return nil
	}
	_, werr := os.process_wait(p, 5 * time.Second)
	if werr != nil {
		_ = os.process_kill(p)
	}
	data, derr := os.read_entire_file(r, context.allocator)
	os.close(r)
	if derr != nil {
		return nil
	}
	defer delete(data)

	lines, _ := strings.split(string(data), "\n")
	defer delete(lines)
	owners: [dynamic]int
	for line in lines {
		pid, ok := strconv.parse_i64(strings.trim_space(line))
		if ok && pid > 1 {
			append(&owners, int(pid))
		}
	}
	return owners[:]
}
