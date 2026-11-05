package supervisor

import "core:os"
import "core:testing"

@(test)
test_exec_basename :: proc(t: ^testing.T) {
	cases := []struct {
		cmd, want: string,
	} {
		{"nginx -g 'daemon off;'", "nginx"},
		{"/usr/sbin/angie", "angie"},
		{"exec /opt/app/bin/server --port 8080", "server"},
		{`"/usr/local/bin/worker"`, "worker"},
		{"", ""},
	}
	for c in cases {
		got := exec_basename(c.cmd)
		testing.expectf(t, got == c.want, "%q: got %q, want %q", c.cmd, got, c.want)
	}
}

@(test)
test_read_pid_file :: proc(t: ^testing.T) {
	path := "/tmp/gorch-pid-test.txt"
	_ = os.remove(path)
	defer os.remove(path)

	if f, err := os.open(path, {.Write, .Create, .Trunc}); err == nil {
		_, _ = os.write_string(f, "4321\n")
		os.close(f)
	} else {
		testing.fail_now(t, "cannot create the temporary pid file")
	}
	testing.expect_value(t, read_pid_file(path), 4321)

	if f, err := os.open(path, {.Write, .Create, .Trunc}); err == nil {
		_, _ = os.write_string(f, "not-a-pid\n")
		os.close(f)
	}
	testing.expect_value(t, read_pid_file(path), 0)
	testing.expect_value(t, read_pid_file("/tmp/gorch-no-such-pid-file"), 0)
}
