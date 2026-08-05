package cron

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const defaultJobTimeout = 5 * time.Minute

// JobType classifies jobs matching Rust's CronJob type system.
type JobType string

const (
	JobTypeAgent   JobType = "agent"
	JobTypeShell   JobType = "shell"
	JobTypeBuiltin JobType = "builtin"
)

// Job is a scheduled task.
type Job struct {
	ID       string
	Name     string
	Schedule string // simplified or standard 5-field cron: "min hour dom month dow"
	Handler  func(ctx context.Context) error
	LastRun  time.Time
	NextRun  time.Time
	Enabled  bool
	JobType  JobType // agent, shell, builtin
	// AgentPrompt is used when JobType is Agent.
	AgentPrompt string
	// ShellCommand is used when JobType is Shell.
	ShellCommand string
	// DeliveryChannel if set, cron output will be delivered to this channel.
	DeliveryChannel string
}

// ShellRunner executes a shell command and returns its output.
type ShellRunner func(ctx context.Context, command string) (string, error)

// Scheduler manages cron jobs.
type Scheduler struct {
	mu          sync.RWMutex
	jobs        map[string]*Job
	running     map[string]bool // job IDs currently executing (prevents overlap)
	store       *JobStore       // optional SQLite persistence
	log         *slog.Logger
	stop        chan struct{}
	stopOnce    sync.Once
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
	sendFn      ChatSender  // for agent-type jobs created by tools
	shellRunner ShellRunner // for shell-type jobs
}

func New(log *slog.Logger) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		jobs:    make(map[string]*Job),
		running: make(map[string]bool),
		log:     log,
		stop:    make(chan struct{}),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// WithStore attaches a SQLite-backed JobStore for persistence.
func (s *Scheduler) WithStore(db *sql.DB) *Scheduler {
	store, err := NewJobStore(db)
	if err != nil {
		s.log.Warn("cron store initialization failed, running without persistence", "error", err)
		return s
	}
	s.store = store
	// Load previously persisted jobs.
	jobs, err := store.LoadJobs()
	if err != nil {
		s.log.Warn("cron load jobs failed", "error", err)
		return s
	}
	for _, j := range jobs {
		// Recalculate NextRun from schedule.
		j.NextRun = s.nextRun(j.Schedule)
		if j.ID == "" {
			j.ID = uuid.New().String()
		}
		s.jobs[j.ID] = j
	}
	s.log.Info("cron loaded persisted jobs", "count", len(jobs))
	return s
}

// Store returns the JobStore, or nil if persistence is not configured.
func (s *Scheduler) Store() *JobStore { return s.store }

// WithChatSender sets the ChatSender for agent-type cron jobs created by tools.
func (s *Scheduler) WithChatSender(fn ChatSender) *Scheduler { s.sendFn = fn; return s }

// WithShellRunner sets the ShellRunner for shell-type cron jobs.
func (s *Scheduler) WithShellRunner(fn ShellRunner) *Scheduler { s.shellRunner = fn; return s }

// Add registers a cron job and persists to store if configured.
func (s *Scheduler) Add(job *Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job.ID == "" {
		job.ID = uuid.New().String()
	}
	job.NextRun = s.nextRun(job.Schedule)
	s.jobs[job.ID] = job
	if s.store != nil {
		if err := s.store.SaveJob(job); err != nil {
			s.log.Warn("cron save job failed", "id", job.ID, "error", err)
		}
	}
}

// Remove deletes a job from memory and the persistent store.
func (s *Scheduler) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.jobs, id)
	if s.store != nil {
		if err := s.store.DeleteJob(id); err != nil {
			s.log.Warn("cron delete job failed", "id", id, "error", err)
		}
	}
}

// Run triggers a job immediately by ID, regardless of schedule.
func (s *Scheduler) Run(id string) error {
	s.mu.RLock()
	job, ok := s.jobs[id]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("job %q not found", id)
	}
	if job.Handler == nil {
		return fmt.Errorf("job %q has no handler", id)
	}

	// Prevent overlapping runs.
	s.mu.Lock()
	if s.running[id] {
		s.mu.Unlock()
		return fmt.Errorf("job %q is already running", id)
	}
	s.running[id] = true
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.running, id)
			s.mu.Unlock()
		}()
		if err := job.Handler(s.ctx); err != nil {
			s.log.Warn("cron job failed", "id", id, "error", err)
		}
	}()

	job.LastRun = time.Now()
	return nil
}

// List returns all registered jobs.
func (s *Scheduler) List() []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		result = append(result, j)
	}
	return result
}

// Start begins the cron loop (10s tick).
func (s *Scheduler) Start() {
	s.wg.Add(1)
	go s.loop()
	s.log.Info("cron scheduler started")
}

// Stop shuts down the scheduler.
func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() {
		s.cancel()
		close(s.stop)
		s.wg.Wait()
		s.log.Info("cron scheduler stopped")
	})
}

func (s *Scheduler) loop() {
	defer s.wg.Done()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.tick()
		case <-s.stop:
			return
		}
	}
}

func (s *Scheduler) tick() {
	// Copy job snapshots under the lock so reads of NextRun and Enabled are
	// synchronized with writes from Add / handler completion.
	s.mu.RLock()
	type jobSnapshot struct {
		job      *Job
		enabled  bool
		nextRun  time.Time
		schedule string
	}
	snapshots := make([]jobSnapshot, 0, len(s.jobs))
	for _, j := range s.jobs {
		snapshots = append(snapshots, jobSnapshot{
			job:      j,
			enabled:  j.Enabled,
			nextRun:  j.NextRun,
			schedule: j.Schedule,
		})
	}
	s.mu.RUnlock()

	now := time.Now()
	for _, snap := range snapshots {
		if !snap.enabled || now.Before(snap.nextRun) {
			continue
		}
		// Prevent overlapping runs: skip if the job is still executing
		// from a previous tick (handler took longer than tick interval).
		s.mu.Lock()
		if s.running[snap.job.ID] {
			s.mu.Unlock()
			s.log.Warn("skipping cron job — still running", "id", snap.job.ID, "name", snap.job.Name)
			continue
		}
		s.running[snap.job.ID] = true
		s.mu.Unlock()

		s.log.Info("running cron job", "id", snap.job.ID, "name", snap.job.Name)
		ctx, cancel := context.WithTimeout(s.ctx, defaultJobTimeout)
		var err error
		switch snap.job.JobType {
		case JobTypeShell:
			if snap.job.ShellCommand == "" {
				err = fmt.Errorf("shell job has no command")
			} else if s.shellRunner != nil {
				_, err = s.shellRunner(ctx, snap.job.ShellCommand)
			} else {
				err = fmt.Errorf("no shell runner configured")
			}
		default:
			if snap.job.Handler == nil {
				err = fmt.Errorf("job has no handler")
			} else {
				err = snap.job.Handler(ctx)
			}
		}
		cancel()

		s.mu.Lock()
		delete(s.running, snap.job.ID)
		if err != nil {
			s.log.Error("cron job failed", "id", snap.job.ID, "error", err)
		}
		snap.job.LastRun = now
		snap.job.NextRun = s.nextRun(snap.schedule)
		s.mu.Unlock()
	}
}

func (s *Scheduler) nextRun(schedule string) time.Time {
	now := time.Now()

	// Simplified schedules.
	switch schedule {
	case "hourly", "1h":
		return now.Add(1 * time.Hour)
	case "daily", "24h":
		return now.Add(24 * time.Hour)
	}

	// Interval-based schedules.
	if d, err := parseInterval(schedule); err == nil {
		return now.Add(d)
	}

	// Full 5-field cron expression: "min hour dom month dow"
	if next, ok := parseCronExpr(schedule, now); ok {
		return next
	}

	return now.Add(1 * time.Hour)
}

// parseCronExpr parses a standard 5-field cron expression and returns the
// next firing time after `now`. Returns zero time if the expression is invalid.
// Fields: minute (0-59), hour (0-23), day-of-month (1-31), month (1-12),
// day-of-week (0-6, 0=Sunday). Supports: wildcard (*), specific values,
// comma-separated lists, step values (*/N), and ranges (M-N).
func parseCronExpr(expr string, now time.Time) (time.Time, bool) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return time.Time{}, false
	}

	// Quick check: if every field is a digit or wildcard, it's not an interval.
	hasCronSyntax := false
	for _, f := range fields {
		if strings.ContainsAny(f, "*,/-") || len(f) > 2 {
			hasCronSyntax = true
			break
		}
	}
	if !hasCronSyntax {
		return time.Time{}, false
	}

	// Search forward up to 366 days for the next match.
	// Start from the next minute.
	candidate := now.Truncate(time.Minute).Add(time.Minute)
	deadline := now.Add(366 * 24 * time.Hour)

	for candidate.Before(deadline) {
		if cronFieldMatches(fields[0], candidate.Minute(), 0, 59) &&
			cronFieldMatches(fields[1], candidate.Hour(), 0, 23) &&
			cronFieldMatches(fields[2], candidate.Day(), 1, 31) &&
			cronFieldMatches(fields[3], int(candidate.Month()), 1, 12) &&
			cronFieldMatches(fields[4], int(candidate.Weekday()), 0, 6) {
			return candidate, true
		}
		candidate = candidate.Add(time.Minute)
	}

	return time.Time{}, false
}

// cronFieldMatches checks whether a value matches a cron field expression.
func cronFieldMatches(field string, value, min, max int) bool {
	// Handle comma-separated lists.
	parts := strings.Split(field, ",")
	for _, part := range parts {
		if cronPartMatches(strings.TrimSpace(part), value, min, max) {
			return true
		}
	}
	return false
}

// cronPartMatches checks a single cron field part (no commas).
func cronPartMatches(part string, value, min, max int) bool {
	if part == "*" {
		return true
	}

	// Step values: */N or M-N/N
	step := 1
	if idx := strings.Index(part, "/"); idx >= 0 {
		s, err := strconv.Atoi(part[idx+1:])
		if err != nil || s <= 0 {
			return false
		}
		step = s
		part = part[:idx]
	}

	if part == "*" {
		return (value-min)%step == 0
	}

	// Range: M-N
	if idx := strings.Index(part, "-"); idx >= 0 {
		lo, err1 := strconv.Atoi(part[:idx])
		hi, err2 := strconv.Atoi(part[idx+1:])
		if err1 != nil || err2 != nil {
			return false
		}
		if value < lo || value > hi {
			return false
		}
		return (value-lo)%step == 0
	}

	// Single value.
	v, err := strconv.Atoi(part)
	if err != nil {
		return false
	}
	return v == value
}

// FormatNextRun returns a human-readable description of the next run time.
func FormatNextRun(job *Job) string {
	if !job.Enabled {
		return "disabled"
	}
	return fmt.Sprintf("%s (%s)", job.NextRun.Format(time.RFC3339), job.Schedule)
}

// parseInterval handles patterns: "*/5m", "5m", "30s", "2h", "*/30s"
func parseInterval(s string) (time.Duration, error) {
	s = strings.TrimPrefix(s, "*/") // strip recurring prefix

	var numStr, unit string
	for i, c := range s {
		if c < '0' || c > '9' {
			numStr = s[:i]
			unit = s[i:]
			break
		}
	}
	if numStr == "" {
		numStr = s
	}

	n, err := strconv.Atoi(numStr)
	if err != nil || n <= 0 {
		return 0, err
	}

	switch unit {
	case "s", "sec", "second", "seconds":
		return time.Duration(n) * time.Second, nil
	case "m", "min", "minute", "minutes":
		return time.Duration(n) * time.Minute, nil
	case "h", "hr", "hour", "hours":
		return time.Duration(n) * time.Hour, nil
	default:
		// If no unit, treat as minutes (backwards compat)
		return time.Duration(n) * time.Minute, nil
	}
}
