package cron

import (
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

const (
	// tickInterval is how often the scheduler compares the wall clock against
	// each job's next run time.
	//
	// This scheduler deliberately polls the wall clock instead of relying on a
	// single long-lived time.Timer (as robfig/cron's engine does). Go timers do
	// not advance while the machine is asleep, so a job scheduled during a sleep
	// window was previously skipped entirely and only ran later, at whatever
	// moment accumulated *awake* time happened to catch up. Re-checking the wall
	// clock on every tick means a missed run is detected as soon as the machine
	// wakes and is executed once ("catch-up").
	tickInterval = 250 * time.Millisecond

	// maxHistory is the number of execution records retained per service.
	maxHistory = 10
)

// job is a single registered cron task.
type job struct {
	name       string
	expression string
	timezone   string
	schedule   cron.Schedule
	loc        *time.Location
	fn         func()
	next       time.Time
}

// Scheduler runs cron tasks on a wall-clock driven loop.
//
// It keeps the public API of the previous robfig/cron-backed implementation but
// owns its own run loop so that schedules remain accurate across system sleep /
// suspend and so that schedule changes take effect immediately.
type Scheduler struct {
	parser cron.Parser

	mu      sync.RWMutex
	jobs    map[string]*job
	records map[string][]CronExecutionRecord
	running map[string]bool

	// Run-loop state, guarded by loopMu.
	loopMu  sync.Mutex
	started bool
	stopCh  chan struct{}
	doneCh  chan struct{}
	wg      sync.WaitGroup

	// nowFunc returns the current wall-clock time. It exists so tests can
	// simulate the machine waking from sleep (wall clock jumping forward)
	// instead of sleeping for real.
	nowFunc func() time.Time
}

type CronExecutionRecord struct {
	Service   string     `json:"service"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
	ExitCode  *int       `json:"exitCode,omitempty"`
	Status    string     `json:"status"`
	Pid       int        `json:"pid"`
}

func NewScheduler() *Scheduler {
	return &Scheduler{
		parser: cron.NewParser(
			cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
		),
		jobs:    make(map[string]*job),
		records: make(map[string][]CronExecutionRecord),
		running: make(map[string]bool),
	}
}

// now returns the current wall-clock time.
func (s *Scheduler) now() time.Time {
	if s.nowFunc != nil {
		return s.nowFunc()
	}
	return time.Now()
}

// AddJob registers (or replaces) a cron job. The new schedule takes effect
// immediately, even while the scheduler is running.
//
// The expression is validated before any state is mutated, so a bad expression
// can never destroy an already-registered job.
func (s *Scheduler) AddJob(name string, expression string, timezone string, fn func()) error {
	loc := time.Local
	if timezone != "" {
		parsed, err := time.LoadLocation(timezone)
		if err != nil {
			return fmt.Errorf("invalid timezone '%s': %w", timezone, err)
		}
		loc = parsed
	}

	sched, err := s.parser.Parse(expression)
	if err != nil {
		return fmt.Errorf("invalid cron expression '%s': %w", expression, err)
	}

	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Re-registering an unchanged schedule must NOT reset the next run time.
	// rebuildCronScheduler re-adds every job on each config update, and pushing
	// `next` forward on those no-op rebuilds would delay (or entirely starve)
	// the task: an unrelated "Apply" just before the due time would silently
	// postpone the execution by a whole interval.
	if old, ok := s.jobs[name]; ok && old.expression == expression && old.timezone == timezone {
		old.schedule = sched
		old.loc = loc
		old.fn = fn
		return nil
	}

	s.jobs[name] = &job{
		name:       name,
		expression: expression,
		timezone:   timezone,
		schedule:   sched,
		loc:        loc,
		fn:         fn,
		next:       sched.Next(now.In(loc)),
	}
	return nil
}

// Start launches the run loop. It is a no-op if already started, and a stopped
// scheduler may be started again.
func (s *Scheduler) Start() {
	s.loopMu.Lock()
	defer s.loopMu.Unlock()

	if s.started {
		return
	}
	s.started = true
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	go s.runLoop()
}

// Stop shuts the run loop down and waits for in-flight jobs to finish.
func (s *Scheduler) Stop() {
	s.loopMu.Lock()
	if !s.started {
		s.loopMu.Unlock()
		return
	}
	s.started = false
	close(s.stopCh)
	doneCh := s.doneCh
	s.loopMu.Unlock()

	<-doneCh
	s.wg.Wait()
}

func (s *Scheduler) runLoop() {
	defer close(s.doneCh)

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.runDue(s.now())
		}
	}
}

// runDue fires every job whose next run time has been reached, using the given
// wall-clock time.
//
// A job whose next time has already passed (the machine was asleep, or the
// process was suspended) is run exactly once and then rescheduled from now —
// missed occurrences are collapsed into a single catch-up run rather than
// replaying each one.
func (s *Scheduler) runDue(now time.Time) {
	s.mu.Lock()
	var due []*job
	for _, j := range s.jobs {
		if now.Before(j.next) {
			continue
		}
		j.next = j.schedule.Next(now.In(j.loc))
		due = append(due, j)
	}
	s.mu.Unlock()

	for _, j := range due {
		s.wg.Add(1)
		go func(j *job) {
			defer s.wg.Done()
			s.fire(j)
		}(j)
	}
}

// fire runs a single job, skipping it (and recording an "overlap") if a
// previous run of the same job is still in progress.
func (s *Scheduler) fire(j *job) {
	s.mu.Lock()
	if s.running[j.name] {
		s.mu.Unlock()
		s.RecordExecution(j.name, CronExecutionRecord{
			Service:   j.name,
			StartedAt: time.Now(),
			Status:    "overlap",
		})
		return
	}
	s.running[j.name] = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.running, j.name)
		s.mu.Unlock()
	}()

	j.fn()
}

// JobCount returns the number of registered cron jobs.
func (s *Scheduler) JobCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.jobs)
}

// RemoveJob removes a cron job by name. It returns true if the job existed.
func (s *Scheduler) RemoveJob(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.jobs[name]; !ok {
		return false
	}
	delete(s.jobs, name)
	return true
}

// HasJob reports whether a job with the given name is registered.
func (s *Scheduler) HasJob(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.jobs[name]
	return ok
}

// JobNames returns the names of all registered jobs.
func (s *Scheduler) JobNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.jobs))
	for name := range s.jobs {
		names = append(names, name)
	}
	return names
}

// JobNext returns the next scheduled run time of a job, or the zero time if the
// job is not registered.
func (s *Scheduler) JobNext(name string) time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if j, ok := s.jobs[name]; ok {
		return j.next
	}
	return time.Time{}
}

func (s *Scheduler) RecordExecution(name string, record CronExecutionRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.records[name] = append(s.records[name], record)
	if len(s.records[name]) > maxHistory {
		s.records[name] = s.records[name][len(s.records[name])-maxHistory:]
	}
}

func (s *Scheduler) GetHistory(name string) []CronExecutionRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	records, ok := s.records[name]
	if !ok {
		return []CronExecutionRecord{}
	}

	result := make([]CronExecutionRecord, len(records))
	copy(result, records)
	return result
}
