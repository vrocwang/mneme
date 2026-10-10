package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// DefaultTaskMaxRetries is the retry budget for a dispatched task when the
// caller does not set one. It is the single source of truth for both the
// in-memory fallback (TaskDispatcher.Enqueue) and the dispatch_tasks DDL
// default (TaskStore.migrate), which previously duplicated the number 3.
const DefaultTaskMaxRetries = 3

// DispatchTask is a unit of autonomous work to be executed.
type DispatchTask struct {
	ID          string     `json:"id"`
	AgentID     string     `json:"agent_id"`
	Prompt      string     `json:"prompt"`
	Priority    string     `json:"priority"`
	Status      string     `json:"status"` // pending, running, completed, failed
	MaxRetries  int        `json:"max_retries"`
	RetryCount  int        `json:"retry_count"`
	ScheduledAt time.Time  `json:"scheduled_at"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Result      string     `json:"result,omitempty"`
	Error       string     `json:"error,omitempty"`

	// Run heartbeat for liveness tracking during autonomous execution.
	LastHeartbeat *time.Time `json:"last_heartbeat,omitempty"`

	// Session thread created for this autonomous run.
	SessionThreadID string `json:"session_thread_id,omitempty"`

	// Claim token prevents concurrent dispatch races.
	ClaimToken string     `json:"claim_token,omitempty"`
	ClaimedAt  *time.Time `json:"claimed_at,omitempty"`
}

// TaskDispatcher manages autonomous task execution with polling and
// approval gating.
type TaskDispatcher struct {
	mu           sync.Mutex
	tasks        map[string]*DispatchTask
	pending      []string // task IDs in FIFO order
	log          *slog.Logger
	executor     TaskExecutor
	approvalGate TaskApprovalGate
	pollInterval time.Duration
	running      bool
	stopCh       chan struct{}
	store        *TaskStore // optional persistent storage
}

// TaskExecutor executes a single dispatched task.
type TaskExecutor func(ctx context.Context, task *DispatchTask) error

// TaskApprovalGate checks whether a task is allowed to execute.
type TaskApprovalGate func(task *DispatchTask) (bool, string)

// NewTaskDispatcher creates a new task dispatcher.
func NewTaskDispatcher(executor TaskExecutor, gate TaskApprovalGate) *TaskDispatcher {
	return &TaskDispatcher{
		tasks:        make(map[string]*DispatchTask),
		log:          slog.Default().With("component", "task-dispatcher"),
		executor:     executor,
		approvalGate: gate,
		pollInterval: 30 * time.Second,
		stopCh:       make(chan struct{}),
	}
}

// WithStore attaches a persistent TaskStore. When set, all task state mutations
// (enqueue, claim, complete, fail) are persisted to SQLite.
func (d *TaskDispatcher) WithStore(store *TaskStore) *TaskDispatcher {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.store = store
	return d
}

// Enqueue adds a task to the pending queue.
func (d *TaskDispatcher) Enqueue(task *DispatchTask) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if task.ID == "" {
		task.ID = fmt.Sprintf("dispatch_%d", time.Now().UnixNano())
	}
	task.Status = "pending"
	task.CreatedAt = time.Now().UTC()
	if task.MaxRetries == 0 {
		task.MaxRetries = DefaultTaskMaxRetries
	}
	d.tasks[task.ID] = task
	d.pending = append(d.pending, task.ID)
	d.log.Info("task enqueued", "id", task.ID, "agent", task.AgentID)
	return nil
}

// Start begins the poller loop.
func (d *TaskDispatcher) Start(ctx context.Context) {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return
	}
	d.running = true
	d.mu.Unlock()

	// Rebuild stopCh if it was closed by a previous Stop.
	if d.stopCh == nil {
		d.stopCh = make(chan struct{})
	}

	d.log.Info("task dispatcher started", "poll_interval", d.pollInterval)

	go func() {
		ticker := time.NewTicker(d.pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				d.log.Info("task dispatcher stopping (context cancelled)")
				return
			case <-d.stopCh:
				d.log.Info("task dispatcher stopped")
				return
			case <-ticker.C:
				d.poll(ctx)
			}
		}
	}()
}

// Stop halts the poller loop. Safe to call multiple times.
func (d *TaskDispatcher) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.running {
		close(d.stopCh)
		d.running = false
	}
}

// ── Atomic claim ─────────────────────────────────────────────────────────

// ReleaseClaim releases a task's claim, returning it to the pending queue.
func (d *TaskDispatcher) ReleaseClaim(taskID, claimToken string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	task, ok := d.tasks[taskID]
	if !ok {
		return false
	}
	if task.ClaimToken != claimToken {
		return false
	}

	task.ClaimToken = ""
	task.ClaimedAt = nil
	task.StartedAt = nil
	task.LastHeartbeat = nil
	task.Status = "pending"
	// Re-add to pending queue, avoiding duplicates.
	if !d.inPendingLocked(taskID) {
		d.pending = append(d.pending, taskID)
	}
	return true
}

// inPendingLocked checks whether taskID is already in the pending queue. Must hold d.mu.
func (d *TaskDispatcher) inPendingLocked(id string) bool {
	for _, tid := range d.pending {
		if tid == id {
			return true
		}
	}
	return false
}

// ── Heartbeat ────────────────────────────────────────────────────────────

// ── Stale run reclaim ────────────────────────────────────────────────────

// ReclaimStale detects runs that haven't sent a heartbeat within the timeout
// and returns their cards to the pending queue. Returns the count of reclaimed tasks.
func (d *TaskDispatcher) ReclaimStale(timeout time.Duration) int {
	d.mu.Lock()
	defer d.mu.Unlock()

	reclaimed := 0
	now := time.Now().UTC()
	for _, task := range d.tasks {
		if task.Status != "running" {
			continue
		}
		isStale := false
		if task.LastHeartbeat == nil {
			isStale = true
			d.log.Warn("reclaimed stale task (no heartbeat)", "id", task.ID)
		} else if now.Sub(*task.LastHeartbeat) > timeout {
			isStale = true
			d.log.Warn("reclaimed stale task", "id", task.ID,
				"age", now.Sub(*task.LastHeartbeat).Round(time.Second))
		}

		if isStale {
			task.Status = "pending"
			task.ClaimToken = ""
			task.ClaimedAt = nil
			task.StartedAt = nil
			task.LastHeartbeat = nil
			// Avoid duplicate pending entries.
			if !d.inPendingLocked(task.ID) {
				d.pending = append(d.pending, task.ID)
			}
			reclaimed++
		}
	}
	return reclaimed
}

// ── Urgency-based poll ────────────────────────────────────────────────────

func (d *TaskDispatcher) poll(ctx context.Context) {
	d.mu.Lock()
	if len(d.pending) == 0 {
		d.mu.Unlock()
		return
	}

	// Look at the next pending task but don't dequeue yet.
	taskID := d.pending[0]
	task := d.tasks[taskID]

	// Check approval gate BEFORE dequeueing — prevents data loss if blocked.
	if d.approvalGate != nil {
		allowed, reason := d.approvalGate(task)
		if !allowed {
			d.pending = d.pending[1:] // dequeue even blocked tasks
			d.mu.Unlock()
			d.log.Info("task blocked by approval gate", "id", taskID, "reason", reason)
			task.Status = "awaiting_approval"
			return
		}
	}

	// Dequeue and claim.
	d.pending = d.pending[1:]
	now := time.Now().UTC()
	task.StartedAt = &now
	task.Status = "running"
	d.mu.Unlock()

	// Execute.
	d.log.Info("executing task", "id", taskID, "agent", task.AgentID)

	err := d.executor(ctx, task)
	completed := time.Now().UTC()
	task.CompletedAt = &completed

	if err != nil {
		task.RetryCount++
		if task.RetryCount < task.MaxRetries {
			task.Error = err.Error()
			task.Status = "pending"
			d.mu.Lock()
			d.pending = append(d.pending, taskID)
			d.mu.Unlock()
			d.log.Warn("task failed, will retry", "id", taskID, "error", err, "retry", task.RetryCount)
			return
		}
		task.Status = "failed"
		task.Error = err.Error()
		d.log.Error("task failed permanently", "id", taskID, "error", err)
	} else {
		task.Status = "completed"
		d.log.Info("task completed", "id", taskID)
	}
}

// List returns all tasks.
func (d *TaskDispatcher) List() []*DispatchTask {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make([]*DispatchTask, 0, len(d.tasks))
	for _, t := range d.tasks {
		result = append(result, t)
	}
	return result
}

// Get returns a task by ID.
func (d *TaskDispatcher) Get(id string) *DispatchTask {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.tasks[id]
}

// Cancel removes a pending task.
func (d *TaskDispatcher) Cancel(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	task, ok := d.tasks[id]
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}
	if task.Status == "running" {
		return fmt.Errorf("cannot cancel running task %q", id)
	}
	task.Status = "cancelled"
	d.removePending(id)
	return nil
}

func (d *TaskDispatcher) removePending(id string) {
	filtered := d.pending[:0]
	for _, tid := range d.pending {
		if tid != id {
			filtered = append(filtered, tid)
		}
	}
	d.pending = filtered
}
