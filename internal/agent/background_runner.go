package agent

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// BackgroundProgressEvent is emitted during async agent execution to report
// progress, completion, or failure.
type BackgroundProgressEvent struct {
	TaskID       string      `json:"task_id"`
	CheckPointID string      `json:"checkpoint_id"`
	Status       string      `json:"status"` // running, interrupted, completed, failed, cancelled
	Message      string      `json:"message"`
	TokenCount   int         `json:"token_count"`
	ToolCalls    int         `json:"tool_calls"`
	Result       *TurnResult `json:"result,omitempty"`
	Error        string      `json:"error,omitempty"`
	Timestamp    time.Time   `json:"timestamp"`
}

// BackgroundRunner executes agent tasks asynchronously with checkpoint
// persistence for long-running operations. Tasks can be paused via
// context cancellation and resumed from their last checkpoint.
type BackgroundRunner struct {
	runner interface {
		QueryWithCheckpoint(ctx context.Context, threadID, checkPointID, userMessage string) (*TurnResult, error)
		Resume(ctx context.Context, threadID, checkPointID string) (*TurnResult, error)
	}
	log *slog.Logger

	mu         sync.Mutex
	activeRuns map[string]context.CancelFunc // checkPointID -> cancel
}

// NewBackgroundRunner creates a BackgroundRunner wrapping an eino Runner.
func NewBackgroundRunner(runner interface {
	QueryWithCheckpoint(ctx context.Context, threadID, checkPointID, userMessage string) (*TurnResult, error)
	Resume(ctx context.Context, threadID, checkPointID string) (*TurnResult, error)
}, log *slog.Logger) *BackgroundRunner {
	if log == nil {
		log = slog.Default()
	}
	return &BackgroundRunner{
		runner:     runner,
		log:        log,
		activeRuns: make(map[string]context.CancelFunc),
	}
}

// RunAsync starts a background agent execution. It derives a cancellable
// context, registers it, and launches a goroutine that calls the runner
// with a checkpoint ID. Progress events are emitted on the returned channel.
//
// The caller should read the channel until it closes. When the task
// completes, the final event has Status "completed" with the Result field
// populated. On interrupt (context cancelled), Status is "interrupted"
// and the caller can later call Resume to continue.
func (br *BackgroundRunner) RunAsync(ctx context.Context, taskID, prompt string) <-chan BackgroundProgressEvent {
	ch := make(chan BackgroundProgressEvent, 64)
	cpID := "bg_" + taskID

	ctx, cancel := context.WithCancel(ctx)
	br.mu.Lock()
	br.activeRuns[cpID] = cancel
	br.mu.Unlock()

	go func() {
		defer close(ch)
		defer func() {
			br.mu.Lock()
			delete(br.activeRuns, cpID)
			br.mu.Unlock()
			cancel()
		}()

		br.emit(ch, BackgroundProgressEvent{
			TaskID: taskID, CheckPointID: cpID,
			Status: "running", Message: "Task started",
			Timestamp: time.Now(),
		})

		result, err := br.runner.QueryWithCheckpoint(ctx, cpID, cpID, prompt)
		if err != nil {
			if ctx.Err() != nil {
				br.emit(ch, BackgroundProgressEvent{
					TaskID: taskID, CheckPointID: cpID,
					Status:    "interrupted",
					Message:   "Task was cancelled; checkpoint saved for resume",
					Timestamp: time.Now(),
				})
				return
			}
			br.emit(ch, BackgroundProgressEvent{
				TaskID: taskID, CheckPointID: cpID,
				Status: "failed", Error: err.Error(),
				Timestamp: time.Now(),
			})
			return
		}

		br.emit(ch, BackgroundProgressEvent{
			TaskID: taskID, CheckPointID: cpID,
			Status:     "completed",
			Message:    "Task completed successfully",
			TokenCount: result.InputTokens + result.OutputTokens,
			ToolCalls:  len(result.ToolCalls),
			Result:     result,
			Timestamp:  time.Now(),
		})
	}()

	return ch
}

// Resume continues an interrupted background task from its checkpoint.
func (br *BackgroundRunner) Resume(ctx context.Context, taskID, checkPointID string) <-chan BackgroundProgressEvent {
	ch := make(chan BackgroundProgressEvent, 64)

	ctx, cancel := context.WithCancel(ctx)
	br.mu.Lock()
	br.activeRuns[checkPointID] = cancel
	br.mu.Unlock()

	go func() {
		defer close(ch)
		defer func() {
			br.mu.Lock()
			delete(br.activeRuns, checkPointID)
			br.mu.Unlock()
			cancel()
		}()

		br.emit(ch, BackgroundProgressEvent{
			TaskID: taskID, CheckPointID: checkPointID,
			Status: "running", Message: "Resuming from checkpoint",
			Timestamp: time.Now(),
		})

		result, err := br.runner.Resume(ctx, checkPointID, checkPointID)
		if err != nil {
			if ctx.Err() != nil {
				br.emit(ch, BackgroundProgressEvent{
					TaskID: taskID, CheckPointID: checkPointID,
					Status:    "interrupted",
					Message:   "Resume was cancelled",
					Timestamp: time.Now(),
				})
				return
			}
			br.emit(ch, BackgroundProgressEvent{
				TaskID: taskID, CheckPointID: checkPointID,
				Status: "failed", Error: err.Error(),
				Timestamp: time.Now(),
			})
			return
		}

		br.emit(ch, BackgroundProgressEvent{
			TaskID: taskID, CheckPointID: checkPointID,
			Status:     "completed",
			Message:    "Task completed after resume",
			TokenCount: result.InputTokens + result.OutputTokens,
			ToolCalls:  len(result.ToolCalls),
			Result:     result,
			Timestamp:  time.Now(),
		})
	}()

	return ch
}

// Cancel cancels a running background task by its checkpoint ID.
// The task goroutine will exit with an "interrupted" event.
func (br *BackgroundRunner) Cancel(checkPointID string) bool {
	br.mu.Lock()
	cancel, ok := br.activeRuns[checkPointID]
	br.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

// ActiveCount returns the number of currently running background tasks.
func (br *BackgroundRunner) ActiveCount() int {
	br.mu.Lock()
	defer br.mu.Unlock()
	return len(br.activeRuns)
}

func (br *BackgroundRunner) emit(ch chan<- BackgroundProgressEvent, evt BackgroundProgressEvent) {
	select {
	case ch <- evt:
	default:
		br.log.Warn("background runner: progress channel full, dropping event",
			"task_id", evt.TaskID, "status", evt.Status)
	}
}
