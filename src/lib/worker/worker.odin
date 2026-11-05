package worker

// Starting the threads gorch runs on.
//
// Two things core:thread does not do for us:
//
//  1. It sizes the stack from RLIMIT_STACK, which is 8 MiB on glibc, musl and
//     Darwin alike. Every loop in gorch waits on a timer, a child process or a
//     socket; the deepest call path is a few frames of fmt. On a 256 MiB router
//     the difference is hard to miss, and a dashboard holding an SSE stream
//     open is enough to notice it.
//
//  2. When a context is handed to `create_and_start*`, Odin does not build a
//     context for the new thread *nor* destroy the default temp allocator when
//     it ends - the pointer travels with the struct and refers to the calling
//     thread's thread-local arena. A raw pthread that installs its own context
//     avoids that, and `leave` finishes the job by destroying its own arena.
//
// An Entry has to start with `context = runtime.default_context()`: a `proc "c"`
// has no implicit context, and the compiler only creates one in the procedure
// that assigns it. That is also why `enter` cannot live here as a helper.

import "base:runtime"
import "core:sys/posix"

// 512 KiB is what the connection threads already use. The supervisor loop, the
// cron loop and the web listener all fit in a fraction of it.
DEFAULT_STACK_SIZE :: 512 * 1024

// Entry is a plain C pthread entry point.
Entry :: proc "c" (arg: rawptr) -> rawptr

// spawn starts entry(arg) on a detached thread with `stack_size` of stack.
// Returns false when the OS refuses to create the thread.
spawn :: proc(entry: Entry, arg: rawptr, stack_size := uint(DEFAULT_STACK_SIZE)) -> bool {
	attr: posix.pthread_attr_t
	if posix.pthread_attr_init(&attr) != nil {
		return false
	}
	defer posix.pthread_attr_destroy(&attr)
	posix.pthread_attr_setstacksize(&attr, stack_size)
	posix.pthread_attr_setdetachstate(&attr, .CREATE_DETACHED)

	tid: posix.pthread_t
	return posix.pthread_create(&tid, &attr, entry, arg) == nil
}

// leave releases what Odin releases for threads it started itself. Call it last
// in every Entry.
leave :: proc "c" () {
	// Assigning a context here is how a `proc "c"` gets one at all; the thread is
	// on its way out, so replacing it costs nothing.
	context = runtime.default_context()
	// core:os keeps thread-local arenas of its own and frees them through this;
	// it has to run while the temp allocator is still there.
	runtime.run_thread_local_cleaners()
	when !runtime.NO_DEFAULT_TEMP_ALLOCATOR {
		runtime.default_temp_allocator_destroy(&runtime.global_default_temp_allocator_data)
	}
}
