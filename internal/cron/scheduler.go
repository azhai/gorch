package cron

import (
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	cron    *cron.Cron
	records map[string][]CronExecutionRecord
	running map[string]bool
	mu      sync.RWMutex
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
		cron:    cron.New(cron.WithSeconds()),
		records: make(map[string][]CronExecutionRecord),
		running: make(map[string]bool),
	}
}

func (s *Scheduler) AddJob(name string, expression string, timezone string, fn func()) error {
	opts := []cron.Option{cron.WithSeconds()}
	if timezone != "" {
		loc, err := time.LoadLocation(timezone)
		if err != nil {
			return fmt.Errorf("invalid timezone '%s': %w", timezone, err)
		}
		opts = append(opts, cron.WithLocation(loc))
	}

	wrappedFn := func() {
		s.mu.Lock()
		if s.running[name] {
			s.mu.Unlock()
			s.RecordExecution(name, CronExecutionRecord{
				Service: name,
				Status:  "overlap",
			})
			return
		}
		s.running[name] = true
		s.mu.Unlock()

		defer func() {
			s.mu.Lock()
			delete(s.running, name)
			s.mu.Unlock()
		}()

		fn()
	}

	_, err := s.cron.AddFunc(expression, wrappedFn)
	if err != nil {
		return fmt.Errorf("invalid cron expression '%s': %w", expression, err)
	}

	return nil
}

func (s *Scheduler) Start() {
	s.cron.Start()
}

func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
}

func (s *Scheduler) RecordExecution(name string, record CronExecutionRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.records[name] = append(s.records[name], record)
	if len(s.records[name]) > 10 {
		s.records[name] = s.records[name][len(s.records[name])-10:]
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
