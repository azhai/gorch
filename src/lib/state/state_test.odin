package state

import "core:testing"
import "core:os"
import "core:time"

// Each test owns its own file: odin runs test cases on parallel threads.

@(test)
test_roundtrip :: proc(t: ^testing.T) {
	path := "/tmp/gorch_state_roundtrip.bin"
	os.remove(path)

	s, loaded := open(path)
	testing.expect(t, !loaded, "fresh store must not report a load")

	r := Record {
		status     = 2,
		pid        = 4242,
		started_at = 1788000000,
		exit_code  = -1,
		restarts   = 3,
		pid_valid  = true,
	}
	testing.expect(t, save_service(&s, "bakery", r))
	testing.expect(t, save_service(&s, "redis", Record{status = 3, exit_code = 7}))
	testing.expect(t, save(&s))
	close(&s)

	s2, loaded2 := open(path)
	testing.expect(t, loaded2, "snapshot should load")
	defer close(&s2)

	got, ok := load_service(&s2, "bakery")
	testing.expect(t, ok)
	testing.expectf(t, got.pid == r.pid, "pid {} want {}", got.pid, r.pid)
	testing.expectf(t, got.started_at == r.started_at, "started_at {}", got.started_at)
	testing.expectf(t, got.exit_code == r.exit_code, "exit_code {}", got.exit_code)
	testing.expectf(t, got.restarts == r.restarts, "restarts {}", got.restarts)
	testing.expect(t, got.pid_valid)
	testing.expectf(t, got.status == 2, "status {}", got.status)

	second, ok2 := load_service(&s2, "redis")
	testing.expect(t, ok2 && second.pid == 0 && !second.pid_valid)

	_, missing := load_service(&s2, "not-a-service")
	testing.expect(t, !missing, "unknown key must miss")

	os.remove(path)
}

@(test)
test_blob_and_remove :: proc(t: ^testing.T) {
	path := "/tmp/gorch_state_blob.bin"
	os.remove(path)

	s, _ := open(path)
	defer close(&s)

	testing.expect(t, put_blob(&s, "meta:supervisor", transmute([]u8)string("12345")))
	value, ok := get_blob(&s, "meta:supervisor")
	testing.expect(t, ok && string(value) == "12345")
	delete(value)
	testing.expect(t, remove_blob(&s, "meta:supervisor"))
	_, gone := get_blob(&s, "meta:supervisor")
	testing.expect(t, !gone)

	os.remove(path)
}

@(test)
test_save_if_due :: proc(t: ^testing.T) {
	path := "/tmp/gorch_state_due.bin"
	os.remove(path)

	s, _ := open(path)
	defer close(&s)

	// nothing changed yet: no write
	testing.expect(t, !save_if_due(&s, 0))
	testing.expect(t, save_service(&s, "svc", Record{status = 1}))
	// dirty and `every` is 0: writes
	testing.expect(t, save_if_due(&s, 0))
	// clean now: no write even with a zero interval
	testing.expect(t, !save_if_due(&s, 0))
	// dirty again but the interval has not elapsed
	testing.expect(t, save_service(&s, "svc", Record{status = 2}))
	testing.expect(t, !save_if_due(&s, time.Hour))

	os.remove(path)
}
