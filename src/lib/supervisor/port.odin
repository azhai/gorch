package supervisor

// CHECK_PORT.
//
// A daemon that daemonizes itself (nginx, angie) can leave a stale master
// bound to its listen port across a gorch restart. The Go build used lsof to
// find and SIGKILL whoever held the port before starting the service; this is
// the Odin equivalent, without depending on an external binary on Linux.
//
// The pre-flight is a plain bind() probe: on the overwhelmingly common path
// (the port is free) it costs two sockets and no process-table walk. Only when
// something is listening do we ask the platform for the owning pids.

import "core:net"
import "core:sys/posix"
import "core:time"

import glog "../../lib/log"

PORT_WAIT_STEP :: 20 * time.Millisecond
PORT_WAIT_ROUNDS :: 10 // 200 ms is plenty for the kernel to release a socket

// port_is_busy answers "could the child bind this port right now?". Both
// families are probed because a listener may be IPv6-only (IPV6_V6ONLY), and
// net.create_socket does not set SO_REUSEADDR, so an active listener really
// does produce Address_In_Use.
port_is_busy :: proc(port: int) -> bool {
	if port <= 0 || port > 65535 {
		return false
	}
	if sock, err := net.create_socket(.IP4, .TCP); err == .None {
		be := net.bind(sock, net.Endpoint{net.IP4_Any, port})
		net.close(sock)
		if be == .Address_In_Use {
			return true
		}
	}
	if sock, err := net.create_socket(.IP6, .TCP); err == .None {
		be := net.bind(sock, net.Endpoint{net.IP6_Any, port})
		net.close(sock)
		if be == .Address_In_Use {
			return true
		}
	}
	return false
}

// clear_check_port is called with the supervisor write lock held, just before
// the child is spawned. It only kills foreign listeners: gorch itself, pid 1
// and any process gorch currently manages are never signalled, so a
// misconfigured port cannot make a service kill its own dependencies.
clear_check_port :: proc(sv: ^Supervisor, s: ^Service) {
	port := s.spec.check_port
	if port <= 0 || port > 65535 || !port_is_busy(port) {
		return
	}
	owners := port_owners(port)
	defer delete(owners)

	self := int(posix.getpid())
	cleared := 0
	for pid in owners {
		if pid <= 1 || pid == self || managed_pid(sv, pid) {
			continue
		}
		glog.info("clearing CHECK_PORT owner", "service", s.name, "port", port, "pid", pid)
		signal(pid, .SIGKILL)
		cleared += 1
	}
	if cleared == 0 {
		glog.warn(
			"CHECK_PORT is occupied but no foreign owner could be cleared",
			"service",
			s.name,
			"port",
			port,
		)
		return
	}
	// SIGKILL is asynchronous; give the kernel a moment to drop the socket
	// before the child races to bind it.
	for _ in 0 ..< PORT_WAIT_ROUNDS {
		if !port_is_busy(port) {
			return
		}
		time.sleep(PORT_WAIT_STEP)
	}
	glog.warn(
		"CHECK_PORT is still occupied after clearing its owner",
		"service",
		s.name,
		"port",
		port,
	)
}

managed_pid :: proc(sv: ^Supervisor, pid: int) -> bool {
	for _, s in sv.services {
		if s.pid == pid || s.cron_pid == pid {
			return true
		}
	}
	return false
}
