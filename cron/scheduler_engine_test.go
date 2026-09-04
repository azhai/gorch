package cron

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// waitFor drains ch until n events have been received or d elapses,
// returning how many were received.
func waitFor(ch <-chan struct{}, n int, d time.Duration) int {
	deadline := time.After(d)
	got := 0
	for got < n {
		select {
		case <-ch:
			got++
		case <-deadline:
			return got
		}
	}
	return got
}

// ── Wall-clock driven firing ────────────────────────────────

// TestScheduler_FiresOnSchedule verifies a job actually runs on its schedule
// using the real clock.
func TestScheduler_FiresOnSchedule(t *testing.T) {
	s := NewScheduler()
	s.Start()
	defer s.Stop()

	fired := make(chan struct{}, 32)
	if err := s.AddJob("every-second", "* * * * * *", "", func() { fired <- struct{}{} }); err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	if got := waitFor(fired, 2, 5*time.Second); got < 2 {
		t.Fatalf("job fired %d times in 5s, want >= 2", got)
	}
}

// TestScheduler_NoFireBeforeDue verifies a job is not run before its next time.
func TestScheduler_NoFireBeforeDue(t *testing.T) {
	s := NewScheduler()
	clock := time.Date(2026, 8, 29, 10, 0, 0, 0, time.Local)
	s.nowFunc = func() time.Time { return clock }

	fired := make(chan struct{}, 8)
	if err := s.AddJob("hourly", "0 0 * * * *", "", func() { fired <- struct{}{} }); err != nil {
		t.Fatal(err)
	}

	// Still 30 minutes before the due time.
	s.runDue(clock.Add(30 * time.Minute))
	if got := len(fired); got != 0 {
		t.Errorf("job fired %d times before it was due", got)
	}
}

// TestScheduler_CatchUpAfterClockJump is the regression test for the bug where
// scheduled tasks silently never ran: the machine was asleep at the scheduled
// moment. A wall-clock driven scheduler must notice the jump on the next tick
// and run the missed job.
func TestScheduler_CatchUpAfterClockJump(t *testing.T) {
	s := NewScheduler()
	clock := time.Date(2026, 8, 29, 7, 0, 0, 0, time.Local)
	s.nowFunc = func() time.Time { return clock }

	fired := make(chan struct{}, 32)
	// Every hour at :00 → 08:00, 09:00, ... 15:00, 16:00
	if err := s.AddJob("bingwp-up", "0 0 * * * *", "", func() { fired <- struct{}{} }); err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	// Machine asleep 07:00 → 15:30; runs at 08..15 were all missed.
	clock = time.Date(2026, 8, 29, 15, 30, 0, 0, time.Local)
	s.runDue(clock)

	if got := waitFor(fired, 1, time.Second); got != 1 {
		t.Fatalf("missed job did not run after wake: got %d, want 1", got)
	}
	// Missed occurrences collapse into a single catch-up run (not one per hour).
	time.Sleep(50 * time.Millisecond)
	if extra := len(fired); extra != 0 {
		t.Errorf("missed runs should collapse into one, got %d extra", extra)
	}

	// Next run must be rescheduled strictly in the future.
	want := time.Date(2026, 8, 29, 16, 0, 0, 0, time.Local)
	if got := s.JobNext("bingwp-up"); !got.Equal(want) {
		t.Errorf("next run = %v, want %v", got, want)
	}

	// Another tick at the same instant must not fire it again.
	s.runDue(clock)
	if extra := len(fired); extra != 0 {
		t.Errorf("job fired again on the same tick: %d extra", extra)
	}
}

// ── Schedule changes take effect immediately ────────────────

// TestScheduler_ChangedExpressionTakesEffectImmediately verifies that editing a
// job's schedule re-computes its next run time right away.
func TestScheduler_ChangedExpressionTakesEffectImmediately(t *testing.T) {
	s := NewScheduler()
	clock := time.Date(2026, 8, 29, 10, 0, 0, 0, time.Local)
	s.nowFunc = func() time.Time { return clock }

	fired := make(chan struct{}, 8)
	// Originally 03:00 daily.
	if err := s.AddJob("job", "0 0 3 * * *", "", func() { fired <- struct{}{} }); err != nil {
		t.Fatal(err)
	}

	// User changes it to 10:30 and applies.
	clock = time.Date(2026, 8, 29, 10, 5, 0, 0, time.Local)
	if err := s.AddJob("job", "0 30 10 * * *", "", func() { fired <- struct{}{} }); err != nil {
		t.Fatal(err)
	}

	want := time.Date(2026, 8, 29, 10, 30, 0, 0, time.Local)
	if got := s.JobNext("job"); !got.Equal(want) {
		t.Fatalf("after schedule change next = %v, want %v", got, want)
	}

	// And it really fires at the new time.
	s.runDue(want)
	if got := waitFor(fired, 1, time.Second); got != 1 {
		t.Errorf("job did not fire at the newly applied time, got %d", got)
	}
}

// TestAddJob_SameExpressionPreservesNext guards against the starvation bug:
// re-registering an unchanged schedule (rebuildCronScheduler does this on every
// config update) must not push the next run time forward.
func TestAddJob_SameExpressionPreservesNext(t *testing.T) {
	s := NewScheduler()
	clock := time.Date(2026, 8, 29, 10, 0, 0, 0, time.Local)
	s.nowFunc = func() time.Time { return clock }

	if err := s.AddJob("job", "0 30 * * * *", "", func() {}); err != nil {
		t.Fatal(err)
	}
	first := s.JobNext("job")

	// 20 minutes later an unrelated config update triggers a rebuild.
	clock = clock.Add(20 * time.Minute)
	if err := s.AddJob("job", "0 30 * * * *", "", func() {}); err != nil {
		t.Fatal(err)
	}

	if got := s.JobNext("job"); !got.Equal(first) {
		t.Errorf("next run moved from %v to %v on a no-op rebuild", first, got)
	}
}

// TestAddJob_InvalidExpressionKeepsExistingJob verifies AddJob is atomic: a bad
// expression must not destroy an already-registered job.
func TestAddJob_InvalidExpressionKeepsExistingJob(t *testing.T) {
	s := NewScheduler()
	if err := s.AddJob("job", "0 30 * * * *", "", func() {}); err != nil {
		t.Fatal(err)
	}
	before := s.JobNext("job")

	if err := s.AddJob("job", "not a cron expr", "", func() {}); err == nil {
		t.Fatal("expected error for invalid expression")
	}
	if !s.HasJob("job") {
		t.Fatal("existing job was destroyed by a failed AddJob")
	}
	if got := s.JobNext("job"); !got.Equal(before) {
		t.Errorf("next run changed on failed AddJob: %v -> %v", before, got)
	}
}

// ── Overlap protection ──────────────────────────────────────

// TestScheduler_OverlapSkipped verifies that a still-running job is not started
// again, and that the skip is recorded as an "overlap".
func TestScheduler_OverlapSkipped(t *testing.T) {
	s := NewScheduler()
	s.nowFunc = func() time.Time { return time.Date(2026, 8, 29, 10, 0, 0, 0, time.Local) }

	release := make(chan struct{})
	entered := make(chan struct{}, 4)
	var runs int32

	if err := s.AddJob("slow", "* * * * * *", "", func() {
		atomic.AddInt32(&runs, 1)
		entered <- struct{}{}
		<-release
	}); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 8, 29, 10, 0, 1, 0, time.Local)

	// First tick starts the job (which blocks).
	s.runDue(now)
	waitFor(entered, 1, time.Second)

	// Later ticks must skip it while it is still running.
	for i := 2; i <= 5; i++ {
		s.runDue(now.Add(time.Duration(i) * time.Second))
	}
	time.Sleep(50 * time.Millisecond)

	if got := atomic.LoadInt32(&runs); got != 1 {
		t.Errorf("job ran %d times while blocked, want 1", got)
	}

	hist := s.GetHistory("slow")
	var overlaps int
	for _, r := range hist {
		if r.Status == "overlap" {
			overlaps++
		}
	}
	if overlaps == 0 {
		t.Error("expected at least one 'overlap' record")
	}

	close(release)
}

// ── Removal / lifecycle ─────────────────────────────────────

// TestScheduler_RemoveJobStopsFiring verifies a removed job no longer runs.
func TestScheduler_RemoveJobStopsFiring(t *testing.T) {
	s := NewScheduler()
	s.nowFunc = func() time.Time { return time.Date(2026, 8, 29, 10, 0, 0, 0, time.Local) }

	fired := make(chan struct{}, 8)
	if err := s.AddJob("job", "* * * * * *", "", func() { fired <- struct{}{} }); err != nil {
		t.Fatal(err)
	}
	if !s.RemoveJob("job") {
		t.Fatal("RemoveJob returned false for an existing job")
	}
	if s.HasJob("job") {
		t.Error("HasJob should be false after removal")
	}

	s.runDue(time.Date(2026, 8, 29, 10, 5, 0, 0, time.Local))
	if got := len(fired); got != 0 {
		t.Errorf("removed job fired %d times", got)
	}
}

func TestScheduler_RemoveJob_NotExists(t *testing.T) {
	s := NewScheduler()
	if s.RemoveJob("nope") {
		t.Error("RemoveJob should return false for unknown job")
	}
}

func TestScheduler_JobNext_Unknown(t *testing.T) {
	s := NewScheduler()
	if got := s.JobNext("nope"); !got.IsZero() {
		t.Errorf("JobNext for unknown job = %v, want zero time", got)
	}
}

func TestScheduler_StartIsIdempotent(t *testing.T) {
	s := NewScheduler()
	s.Start()
	s.Start() // must not panic or start a second loop
	s.Stop()
	s.Stop() // second Stop must be a no-op, not a panic
}

func TestScheduler_RestartAfterStop(t *testing.T) {
	s := NewScheduler()
	s.Start()
	s.Stop()

	s.Start()
	defer s.Stop()

	fired := make(chan struct{}, 8)
	if err := s.AddJob("job", "* * * * * *", "", func() { fired <- struct{}{} }); err != nil {
		t.Fatal(err)
	}
	if got := waitFor(fired, 1, 5*time.Second); got < 1 {
		t.Error("scheduler did not run jobs after being restarted")
	}
}

// ── Concurrency ─────────────────────────────────────────────

// TestScheduler_ConcurrentAccess exercises the scheduler under the race
// detector: jobs firing while being added/removed/read concurrently.
func TestScheduler_ConcurrentAccess(t *testing.T) {
	s := NewScheduler()
	s.Start()
	defer s.Stop()

	var wg sync.WaitGroup
	var fired int32

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = s.AddJob("j1", "* * * * * *", "", func() { atomic.AddInt32(&fired, 1) })
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			s.HasJob("j1")
			s.JobNames()
			s.JobCount()
			s.JobNext("j1")
			s.GetHistory("j1")
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			s.RemoveJob("j1")
		}
	}()

	wg.Wait()
}
