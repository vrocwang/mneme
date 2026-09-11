package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// TriageEnvelope wraps an incoming webhook or cron event for classification.
type TriageEnvelope struct {
	ID          string    `json:"id"`
	Source      string    `json:"source"` // webhook, cron, channel, manual
	EventKind   string    `json:"event_kind"`
	Payload     string    `json:"payload"`
	ContentType string    `json:"content_type,omitempty"`
	ReceivedAt  time.Time `json:"received_at"`
}

// TriageDecision is the result of classifying an envelope.
type TriageDecision struct {
	Action      TriageAction `json:"action"`
	TargetAgent string       `json:"target_agent,omitempty"`
	TargetSkill string       `json:"target_skill,omitempty"`
	Priority    string       `json:"priority"` // critical, high, normal, low
	Confidence  float64      `json:"confidence"`
	Reason      string       `json:"reason"`
}

// TriageAction is the routing decision.
type TriageAction string

const (
	TriageRoute    TriageAction = "route"    // dispatch to target agent
	TriageDrop     TriageAction = "drop"     // discard (spam, irrelevant)
	TriageDefer    TriageAction = "defer"    // re-queue for later
	TriageEscalate TriageAction = "escalate" // raise to human/system
)

// TriageEvaluator provides the graph's rules stage. Rule-based routing was
// never configured (there is no rule source and no provider setter), so the
// stage now deterministically reports "no match" and lets the graph's LLM
// stage classify; the ensure stage then falls back to escalate.
type TriageEvaluator struct{}

// NewTriageEvaluator creates a new triage evaluator.
func NewTriageEvaluator() *TriageEvaluator { return &TriageEvaluator{} }

// Evaluate reports that no deterministic rule matched. A non-nil decision is
// required by the graph's downstream stages.
func (e *TriageEvaluator) Evaluate(_ *TriageEnvelope) *TriageDecision {
	return &TriageDecision{
		Action:     TriageEscalate,
		Priority:   "normal",
		Confidence: 0,
		Reason:     "no matching rule",
	}
}

// ── Triage Pipeline ─────────────────────────────────────────────────

// TriagePipeline orchestrates the full triage flow: receive → evaluate →
// route → execute. It connects the triage evaluator with the task
// dispatcher for autonomous agent triggering.
//
// When a graphClassifier is set (via WithGraphClassifier or
// NewGraphTriagePipeline), classification uses the compiled eino graph
// instead of the imperative TriageEvaluator path, giving callback
// visibility and optional checkpoint support.
type TriagePipeline struct {
	evaluator  *TriageEvaluator
	dispatcher *TaskDispatcher
	log        *slog.Logger

	// graphClassifier is an optional compiled eino Graph that replaces
	// the imperative evaluator for classification. When set, Process
	// calls it instead of evaluator.Evaluate.
	graphClassifier func(ctx context.Context, input *TriageEnvelope) (*TriageDecision, error)

	// deferred tracks how many times each envelope has been deferred,
	// for bounded retry (max 2 before escalating).
	deferred map[string]int
	mu       sync.Mutex
}

// NewTriagePipeline creates a new triage pipeline.
func NewTriagePipeline(eval *TriageEvaluator, disp *TaskDispatcher) *TriagePipeline {
	return &TriagePipeline{
		evaluator:  eval,
		dispatcher: disp,
		log:        slog.Default().With("component", "triage-pipeline"),
		deferred:   make(map[string]int),
	}
}

// WithGraphClassifier sets a compiled eino graph as the classification
// engine. When set, Process uses the graph instead of the imperative evaluator.
func (p *TriagePipeline) WithGraphClassifier(g func(ctx context.Context, input *TriageEnvelope) (*TriageDecision, error)) *TriagePipeline {
	p.graphClassifier = g
	return p
}

// Stop halts the pipeline's task dispatcher, which otherwise runs for the
// process lifetime. Safe to call on a nil pipeline or one without a dispatcher.
func (p *TriagePipeline) Stop() {
	if p == nil || p.dispatcher == nil {
		return
	}
	p.dispatcher.Stop()
}

// Process evaluates an envelope and dispatches to the target agent.
func (p *TriagePipeline) Process(ctx context.Context, envelope *TriageEnvelope) (*TriageDecision, error) {
	var decision *TriageDecision
	if p.graphClassifier != nil {
		var err error
		decision, err = p.graphClassifier(ctx, envelope)
		if err != nil {
			p.log.Warn("triage graph classifier failed, falling back to evaluator", "error", err)
			decision = p.evaluator.Evaluate(envelope)
		}
	} else {
		decision = p.evaluator.Evaluate(envelope)
	}

	if decision == nil {
		p.log.Warn("triage returned nil decision, dropping envelope", "id", envelope.ID)
		return &TriageDecision{Action: TriageDrop, Priority: "low", Reason: "nil decision from classifier"}, nil
	}

	p.log.Info("triage decision",
		"envelope_id", envelope.ID,
		"action", decision.Action,
		"target", decision.TargetAgent,
		"confidence", decision.Confidence,
	)

	switch decision.Action {
	case TriageRoute:
		if p.dispatcher != nil {
			task := &DispatchTask{
				AgentID:  decision.TargetAgent,
				Prompt:   envelope.Payload,
				Priority: decision.Priority,
			}
			if err := p.dispatcher.Enqueue(task); err != nil {
				return decision, fmt.Errorf("dispatch: %w", err)
			}
		}
	case TriageEscalate:
		p.log.Warn("triage escalation", "envelope_id", envelope.ID, "reason", decision.Reason)
	case TriageDrop:
		p.log.Debug("triage drop", "envelope_id", envelope.ID)
	case TriageDefer:
		// Bounded retry: re-queue up to 2 times before escalating.
		// Matches Rust Deferred { defer_until_ms } semantics.
		p.mu.Lock()
		count := p.deferred[envelope.ID]
		p.deferred[envelope.ID] = count + 1
		p.mu.Unlock()

		if count < 2 {
			p.log.Info("triage deferred, will retry",
				"envelope_id", envelope.ID,
				"attempt", count+1,
				"reason", decision.Reason,
			)
			go func(env *TriageEnvelope, attempt int) {
				time.Sleep(time.Duration(30*(attempt+1)) * time.Second)
				if _, err := p.Process(context.Background(), env); err != nil {
					p.log.Warn("triage deferred retry failed", "envelope_id", env.ID, "error", err)
				}
			}(envelope, count)
		} else {
			p.log.Warn("triage defer limit reached, escalating",
				"envelope_id", envelope.ID,
				"reason", decision.Reason,
			)
			decision.Action = TriageEscalate
			decision.Reason = fmt.Sprintf("deferred %d times: %s", count+1, decision.Reason)
		}
	}

	return decision, nil
}
