package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/simon/mneme/internal/security"
)

// ChatService orchestrates a complete chat turn via the eino Runner pipeline.
// It handles prompt injection detection, delegates execution to the runner,
// persists session records, and fires post-turn callbacks.
type ChatService struct {
	log *slog.Logger

	// runner is the eino execution pipeline. When nil (eino init failed),
	// SendMessage returns an error; the app is degraded.
	runner interface {
		Query(ctx context.Context, threadID, message string) (*TurnResult, error)
		StreamQuery(ctx context.Context, threadID, message string, onEvent func(StreamEvent)) (*TurnResult, error)
	}

	// resolveModel maps a user message to the model name to use for the turn.
	resolveModel func(message string) string

	// saveUserMessage persists a user message before the turn starts.
	saveUserMessage func(threadID string, message string)

	// sessionDB persists session and run-ledger records for auditing and cost analysis.
	sessionDB *SessionDB

	// auditLogger records security events (injection blocks, etc.). Nil-safe.
	auditLogger *security.AuditLogger

	// Post-turn callbacks (nil-safe — skipped when nil).
	postTurn []TurnCallback
	hooks    *PostTurnHookRegistry // unified post-turn hook registry (async, panic-recovered, nil-safe)

	interrupts map[string]chan struct{} // per-thread interrupt channels
	cancelled  map[string]bool          // prevents double-close panic
	mu         sync.Mutex
}

// TurnCallback runs after a successful turn. Errors are logged, not returned.
type TurnCallback func(ctx context.Context, result *TurnResult) error

// ChatServiceConfig holds the dependencies for ChatService.
type ChatServiceConfig struct {
	Log    *slog.Logger
	Runner interface {
		Query(ctx context.Context, threadID, message string) (*TurnResult, error)
		StreamQuery(ctx context.Context, threadID, message string, onEvent func(StreamEvent)) (*TurnResult, error)
	}
	ResolveModel    func(message string) string           // model routing from user message
	SaveUserMessage func(threadID string, message string) // persist user message before turn
	SessionDB       *SessionDB                            // optional persistent session tracking
	AuditLogger     *security.AuditLogger                 // optional security audit logger
}

// SetRunner wires an eino Runner into the ChatService.
func (cs *ChatService) SetRunner(r interface {
	Query(ctx context.Context, threadID, message string) (*TurnResult, error)
	StreamQuery(ctx context.Context, threadID, message string, onEvent func(StreamEvent)) (*TurnResult, error)
}) {
	cs.runner = r
}

// SetAuditLogger wires a security AuditLogger into the ChatService.
func (cs *ChatService) SetAuditLogger(al *security.AuditLogger) {
	cs.auditLogger = al
}

func NewChatService(cfg ChatServiceConfig) *ChatService {
	svc := &ChatService{
		log:             cfg.Log,
		runner:          cfg.Runner,
		resolveModel:    cfg.ResolveModel,
		saveUserMessage: cfg.SaveUserMessage,
		sessionDB:       cfg.SessionDB,
		interrupts:      make(map[string]chan struct{}),
		cancelled:       make(map[string]bool),
	}
	if svc.log == nil {
		svc.log = slog.Default()
	}
	return svc
}

// AddTurnCallback registers a hook to run after each successful turn.
func (cs *ChatService) AddTurnCallback(h TurnCallback) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.postTurn = append(cs.postTurn, h)
}

// SetHookRegistry attaches a PostTurnHookRegistry. Hooks fire after postTurn
// callbacks, asynchronously, with panic recovery. Nil-safe.
func (cs *ChatService) SetHookRegistry(h *PostTurnHookRegistry) { cs.hooks = h }

// turnResultToSnapshot converts a TurnResult into a TurnSnapshot for hooks.
func turnResultToSnapshot(threadID, userMessage string, r *TurnResult) *TurnSnapshot {
	if r == nil {
		return nil
	}
	return &TurnSnapshot{
		ThreadID:     threadID,
		Model:        r.Model,
		UserMessage:  userMessage,
		Response:     r.Response,
		ToolCalls:    r.ToolCalls,
		TotalRounds:  r.Rounds,
		Duration:     r.TotalDuration,
		InputTokens:  r.InputTokens,
		OutputTokens: r.OutputTokens,
		Error:        r.Error,
		CompletedAt:  time.Now(),
	}
}

// SendMessage runs a complete chat turn via the eino Runner.
func (cs *ChatService) SendMessage(ctx context.Context, threadID, message string) (*TurnResult, error) {
	// Server-side prompt injection detection before the message enters the session.
	if decision := security.EnforcePromptInput(message); decision.Verdict == security.VerdictBlock {
		cs.log.Warn("prompt blocked by injection detector",
			"score", decision.Score,
			"thread", threadID,
		)
		if cs.auditLogger != nil {
			cs.auditLogger.Record(security.AuditInjectionBlock, security.AuditEvent{
				Reason: fmt.Sprintf("score=%.2f reasons=%d", decision.Score, len(decision.Reasons)),
			})
		}
		return &TurnResult{
			ThreadID: threadID,
			Response: "Your message was blocked by security policy. Please rephrase your request.",
		}, nil
	} else if decision.Verdict == security.VerdictReview && decision.Score >= 0.60 {
		cs.log.Warn("prompt flagged for review by injection detector",
			"score", decision.Score,
			"reasons", len(decision.Reasons),
			"thread", threadID,
		)
	}

	if cs.saveUserMessage != nil {
		cs.saveUserMessage(threadID, message)
	}

	if cs.runner == nil {
		return &TurnResult{
			ThreadID: threadID,
			Response: "Agent pipeline is not available. The eino runner failed to initialize. Check the logs for details.",
		}, nil
	}

	// Per-turn interrupt channel so CancelMessage can stop a running turn cleanly.
	interruptCh := make(chan struct{})
	cs.mu.Lock()
	cs.interrupts[threadID] = interruptCh
	cs.mu.Unlock()
	defer func() {
		cs.mu.Lock()
		delete(cs.interrupts, threadID)
		delete(cs.cancelled, threadID)
		cs.mu.Unlock()
	}()

	// Run with cancellation support via context.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-interruptCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	result, err := cs.runner.Query(ctx, threadID, message)
	if err != nil {
		cs.recordSessionTurn(threadID, message, nil, err)
		return nil, err
	}

	// Persist session record and run-ledger entry for auditing/cost analysis.
	cs.recordSessionTurn(threadID, message, result, nil)

	if ctx.Value(SkipPostHooksKey{}) == nil {
		cs.mu.Lock()
		hooks := cs.postTurn
		cs.mu.Unlock()
		if len(hooks) > 0 {
			go func() {
				hookCtx := context.WithoutCancel(ctx)
				for _, hook := range hooks {
					if err := hook(hookCtx, result); err != nil {
						cs.log.Warn("post-turn hook failed", "error", err)
					}
				}
			}()
		}
	}
	if cs.hooks != nil {
		cs.hooks.Fire(ctx, turnResultToSnapshot(threadID, message, result))
	}

	return result, nil
}

// SkipPostHooksKey is a context key. When set to true in the context,
// SendMessage skips post-turn hooks (learning, archiving, etc.).
type SkipPostHooksKey struct{}

// WithoutPostHooks returns a context that signals SendMessage to skip post-turn hooks.
func WithoutPostHooks(ctx context.Context) context.Context {
	return context.WithValue(ctx, SkipPostHooksKey{}, true)
}

// StreamMessage runs a chat turn with streaming callbacks via the eino Runner.
func (cs *ChatService) StreamMessage(ctx context.Context, threadID, message string, onEvent func(evt StreamEvent)) {
	// Server-side prompt injection detection (streaming path).
	if decision := security.EnforcePromptInput(message); decision.Verdict == security.VerdictBlock {
		cs.log.Warn("streaming prompt blocked by injection detector", "score", decision.Score, "thread", threadID)
		if cs.auditLogger != nil {
			cs.auditLogger.Record(security.AuditInjectionBlock, security.AuditEvent{
				Reason: fmt.Sprintf("score=%.2f reasons=%d (stream)", decision.Score, len(decision.Reasons)),
			})
		}
		onEvent(StreamEvent{ThreadID: threadID, Type: "error", Content: "Message blocked by security policy.", Done: true})
		return
	}

	if cs.saveUserMessage != nil {
		cs.saveUserMessage(threadID, message)
	}

	if cs.runner == nil {
		onEvent(StreamEvent{ThreadID: threadID, Type: "error", Content: "Agent pipeline is not available. The eino runner failed to initialize.", Done: true})
		return
	}

	go func() {
		result, err := cs.runner.StreamQuery(ctx, threadID, message, func(evt StreamEvent) {
			onEvent(evt)
		})
		if err != nil {
			cs.log.Error("streaming chat failed", "thread_id", threadID, "error", err)
		}
		// Post-turn: persist session and fire callbacks with the full
		// TurnResult (tool calls, token counts, etc.).
		cs.recordSessionTurn(threadID, message, result, err)
		if ctx.Value(SkipPostHooksKey{}) == nil {
			cs.mu.Lock()
			hooks := cs.postTurn
			cs.mu.Unlock()
			if len(hooks) > 0 {
				for _, hook := range hooks {
					if err := hook(ctx, result); err != nil {
						cs.log.Warn("post-turn hook failed", "error", err)
					}
				}
			}
		}
		if cs.hooks != nil {
			cs.hooks.Fire(ctx, turnResultToSnapshot(threadID, message, result))
		}
	}()
}

// Cancel interrupts a running turn for the given thread. If no turn is running,
// the call is a no-op. Safe to call from any goroutine, including multiple times.
func (cs *ChatService) Cancel(threadID string) {
	cs.mu.Lock()
	ch, ok := cs.interrupts[threadID]
	if cs.cancelled[threadID] {
		cs.mu.Unlock()
		return
	}
	cs.cancelled[threadID] = true
	cs.mu.Unlock()
	if ok {
		close(ch)
	}
}

// Send is a simplified version of SendMessage that returns only the response
// text. Satisfies the channels.ChatMessageSender interface.
func (cs *ChatService) Send(ctx context.Context, threadID, message string) (string, error) {
	result, err := cs.SendMessage(ctx, threadID, message)
	if err != nil {
		return "", err
	}
	return result.Response, nil
}

// recordSessionTurn creates or updates a persistent session record and appends a
// run-ledger entry. Nil-safe: when sessionDB is nil the call is a no-op.
func (cs *ChatService) recordSessionTurn(threadID, prompt string, result *TurnResult, turnErr error) {
	if cs.sessionDB == nil {
		return
	}

	// Ensure a session record exists for this thread (idempotent).
	sessionID := threadID
	rec := cs.sessionDB.GetSession(sessionID)
	if rec == nil {
		rec = cs.sessionDB.CreateSession(sessionID, "general", "chat")
	}

	turnIndex := rec.TurnCount + 1
	status := "completed"
	errMsg := ""
	if turnErr != nil {
		status = "failed"
		errMsg = turnErr.Error()
	}

	// Update session counters.
	_ = cs.sessionDB.UpdateSession(sessionID, func(r *SessionRecord) {
		r.ThreadID = threadID
		r.TurnCount++
		if result != nil {
			r.ToolCalls += len(result.ToolCalls)
			r.TotalTokens += int64(result.InputTokens + result.OutputTokens)
			r.TotalCost += float64(result.CostCents) / 100.0
		}
		if turnErr != nil {
			r.Error = errMsg
		}
	})

	// Append a run-ledger entry for this turn.
	entry := RunLedgerEntry{
		RunID:       uuid.New().String(),
		SessionID:   sessionID,
		AgentID:     "general",
		Prompt:      prompt,
		TurnIndex:   turnIndex,
		Status:      status,
		Error:       errMsg,
		StartedAt:   time.Now().UTC(),
		CompletedAt: time.Now().UTC(),
	}
	if result != nil {
		entry.TokensIn = int64(result.InputTokens)
		entry.TokensOut = int64(result.OutputTokens)
		entry.TokensUsed = int64(result.InputTokens + result.OutputTokens)
		entry.CostUSD = float64(result.CostCents) / 100.0
		entry.ToolCalls = len(result.ToolCalls)
	}
	cs.sessionDB.AppendLedger(entry)

	// Mark session as failed on error so the ledger is queryable for outages.
	if turnErr != nil {
		_ = cs.sessionDB.CompleteSession(sessionID, errMsg)
	}
}
