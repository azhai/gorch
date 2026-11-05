package status

import "core:testing"
import "core:time"

// A service without a live pid has no age. This is the rule the dashboard
// relies on to render a dash instead of a fabricated uptime.
@(test)
test_compute_uptime_requires_live_pid :: proc(t: ^testing.T) {
	now := time.to_unix_seconds(time.now())

	running := ServiceStatus {
		status     = .Running,
		pid        = 42,
		started_at = now - 5,
	}
	compute_uptime(&running)
	testing.expect(t, running.has_uptime, "running service with a pid has uptime")
	testing.expect(t, running.uptime >= 4, "uptime counts from started_at")

	no_pid := ServiceStatus {
		status     = .Running,
		pid        = 0,
		started_at = now - 5,
	}
	compute_uptime(&no_pid)
	testing.expect(t, !no_pid.has_uptime, "no pid means no uptime")
	testing.expect_value(t, no_pid.uptime, i64(0))

	stopped := ServiceStatus {
		status     = .Stopped,
		pid        = 42,
		started_at = now - 5,
	}
	compute_uptime(&stopped)
	testing.expect(t, !stopped.has_uptime, "stopped service has no uptime")
	testing.expect_value(t, stopped.uptime, i64(0))
}
