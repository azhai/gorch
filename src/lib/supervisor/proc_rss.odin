package supervisor

// Resident set size. The platform half (proc_rss_linux reads /proc, and
// proc_rss_darwin asks libproc) produces a snapshot of pid -> parent and
// pid -> RSS in MB; this half walks the process tree of a supervised pid and
// sums its descendants in, so a daemon that forked workers (nginx, postgres)
// reports the memory of the whole group and not just its master.

// read_rss_mb reports the resident memory of `pid` and everything it forked,
// in MB. 0 means "unknown" - the process vanished mid-sample or the platform
// read failed - and callers treat that as "keep the previous value".
read_rss_mb :: proc(pid: int) -> f64 {
	if pid <= 0 {
		return 0
	}
	parents, rss, ok := rss_sample()
	if !ok {
		return 0
	}
	defer delete(parents)
	defer delete(rss)

	total: f64
	stack: [dynamic]int
	defer delete(stack)
	// A dynamic-int visited set has no built-in type; map[int]bool is the
	// idiomatic stand-in and costs one hash lookup per node.
	seen: map[int]bool
	defer delete(seen)

	append(&stack, pid)
	for i := 0; i < len(stack); i += 1 {
		cur := stack[i]
		if seen[cur] {
			continue
		}
		seen[cur] = true
		mb, has := rss[cur]
		if !has {
			continue // exited mid-walk; its cost is gone anyway
		}
		total += mb
		for child, parent in parents {
			if parent == cur && !seen[child] {
				append(&stack, child)
			}
		}
	}
	return total
}
