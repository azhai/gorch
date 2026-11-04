#+build linux
package supervisor

import "core:fmt"
import "core:os"
import "core:strconv"
import "core:strings"

// port_owners maps a listening TCP port back to pids using only /proc, which
// is always present on Linux and needs no external lsof/ss binary - important
// for the statically linked musl release.
//
// /proc/net/tcp{,6} exposes the socket inode of every LISTEN socket; the
// symlinks in /proc/<pid>/fd identify a socket fd as "socket:[<inode>]".
port_owners :: proc(port: int) -> []int {
	inodes := listening_socket_inodes(port)
	defer delete(inodes)
	if len(inodes) == 0 {
		return nil
	}

	owners: [dynamic]int
	entries, err := os.read_all_directory_by_path("/proc", context.allocator)
	if err != nil {
		return nil
	}
	defer delete(entries)

	for e in entries {
		pid, ok := strconv.parse_i64(e.name)
		if !ok || pid <= 1 {
			continue // not a process directory
		}
		fds, ferr := os.read_all_directory_by_path(
			fmt.tprintf("/proc/{}/fd", e.name),
			context.allocator,
		)
		if ferr != nil {
			continue // exited, or no permission
		}
		found := false
		for fd in fds {
			link, lerr := os.read_link(
				fmt.tprintf("/proc/{}/fd/{}", e.name, fd.name),
				context.allocator,
			)
			if lerr != nil {
				continue
			}
			ino, parsed := socket_inode(link)
			delete(link, context.allocator)
			if parsed && ino > 0 && ino in inodes {
				found = true
				break
			}
		}
		delete(fds)
		if found {
			append(&owners, int(pid))
		}
	}
	return owners[:]
}

socket_inode :: proc(link: string) -> (u64, bool) {
	prefix :: "socket:["
	if !strings.has_prefix(link, prefix) || !strings.has_suffix(link, "]") {
		return 0, false
	}
	return strconv.parse_u64(link[len(prefix) : len(link) - 1])
}

// listening_socket_inodes collects the inodes of sockets in TCP_LISTEN (0A)
// whose local port matches. Field layout of /proc/net/tcp is fixed by the
// kernel: [1] local_address (hex ip:port), [3] state, [9] inode.
TCP_TABLES :: [2]string{"/proc/net/tcp", "/proc/net/tcp6"}

listening_socket_inodes :: proc(port: int) -> map[u64]bool {
	inodes := make(map[u64]bool)
	for path in TCP_TABLES {
		data, err := os.read_entire_file_from_path(path, context.allocator)
		if err != nil {
			continue
		}
		lines, _ := strings.split(string(data), "\n")
		for line, i in lines {
			if i == 0 {
				continue // column header
			}
			fields := strings.fields(line)
			if len(fields) < 10 || fields[3] != "0A" {
				delete(fields)
				continue
			}
			colon := strings.last_index_byte(fields[1], ':')
			if colon >= 0 {
				p, pok := strconv.parse_u64(fields[1][colon + 1:], 16)
				if pok && int(p) == port {
					if ino, iok := strconv.parse_u64(fields[9]); iok && ino > 0 {
						inodes[ino] = true
					}
				}
			}
			delete(fields)
		}
		delete(lines)
		delete(data)
	}
	return inodes
}
