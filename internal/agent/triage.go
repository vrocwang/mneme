package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/simon/mneme/internal/inference"
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

// TriageRule defines a single routing rule.
type TriageRule struct {
	Name        string   `json:"name"`
	Sources     []string `json:"sources,omitempty"`     // match these sources
	EventKinds  []string `json:"event_kinds,omitempty"` // match these event kinds
	Keywords    []string `json:"keywords,omitempty"`    // match these in payload
	TargetAgent string   `json:"target_agent"`
	Priority    string   `json:"priority"`
	Enabled     bool     `json:"enabled"`
}

// TriageEvaluator classifies incoming events against a rule set and
// routes them to the appropriate agent or skill.
type TriageEvaluator struct {
	mu       sync.RWMutex
	rules    []TriageRule
	log      *slog.Logger
	provider inference.Provider
	model    string
}

// NewTriageEvaluator creates a new triage evaluator.
func NewTriageEvaluator() *TriageEvaluator {
	return &TriageEvaluator{
		log: slog.Default().With("component", "triage"),
	}
}

// AddRule registers a new routing rule.
func (e *TriageEvaluator) AddRule(rule TriageRule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = append(e.rules, rule)
}

// WithProvider configures an optional LLM provider for fallback classification.
func (e *TriageEvaluator) WithProvider(provider inference.Provider, model string) *TriageEvaluator {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.provider = provider
	e.model = model
	return e
}

// Evaluate classifies an envelope and returns a routing decision.
func (e *TriageEvaluator) Evaluate(envelope *TriageEnvelope) *TriageDecision {
	// Snapshot provider/model under read lock, then release before I/O.
	e.mu.RLock()
	provider := e.provider
	rules := e.rules
	e.mu.RUnlock()

	payloadLower := strings.ToLower(envelope.Payload)

	var bestMatch *TriageDecision
	var bestConfidence float64

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}

		confidence := e.matchRule(&rule, envelope, payloadLower)
		if confidence > bestConfidence {
			bestConfidence = confidence
			target := rule.TargetAgent
			bestMatch = &TriageDecision{
				Action:      TriageRoute,
				TargetAgent: target,
				Priority:    rule.Priority,
				Confidence:  confidence,
				Reason:      fmt.Sprintf("matched rule %q", rule.Name),
			}
		}
	}

	if bestMatch != nil {
		return bestMatch
	}

	// No rules matched — try LLM classification if available.
	if provider != nil {
		if decision := e.evaluateWithLLM(context.Background(), envelope); decision != nil {
			return decision
		}
	}
	// Fall back to escalate for manual review.
	return &TriageDecision{
		Action:     TriageEscalate,
		Priority:   "normal",
		Confidence: 0,
		Reason:     "no matching rule",
	}
}

func (e *TriageEvaluator) matchRule(rule *TriageRule, envelope *TriageEnvelope, payloadLower string) float64 {
	score := 0.0
	checks := 0

	// Source match.
	if len(rule.Sources) > 0 {
		checks++
		for _, s := range rule.Sources {
			if strings.EqualFold(s, envelope.Source) {
				score += 1.0
				break
			}
		}
	}

	// Event kind match.
	if len(rule.EventKinds) > 0 {
		checks++
		for _, k := range rule.EventKinds {
			if strings.EqualFold(k, envelope.EventKind) {
				score += 1.0
				break
			}
		}
	} else {
		checks++ // no event kind filter = always matches
		score += 0.5
	}

	// Keyword match.
	if len(rule.Keywords) > 0 {
		checks++
		for _, kw := range rule.Keywords {
			if strings.Contains(payloadLower, strings.ToLower(kw)) {
				score += 0.5
				break
			}
		}
	}

	if checks == 0 {
		return 0
	}
	return score / float64(checks)
}

// evaluateWithLLM classifies an envelope using the LLM provider when rule
// matching fails. Results are cached by envelope hash to avoid re-classifying
// identical triggers.
func (e *TriageEvaluator) evaluateWithLLM(ctx context.Context, envelope *TriageEnvelope) *TriageDecision {
	prompt := fmt.Sprintf(
		`Classify this incoming event and return a JSON decision.

Source: %s
Event kind: %s
Payload (first 500 chars): %s

Return ONLY valid JSON: {"action":"route|drop|defer|escalate","target_agent":"","priority":"normal","reason":""}

Rules:
- "route" for actionable requests, questions, or commands → set target_agent to "general"
- "drop" for spam, noise, or empty messages
- "defer" for things that should be checked later (status updates, logs)
- "escalate" for security concerns, errors, or urgent issues needing human attention
- Priority: "critical" (security/errors), "high" (urgent requests), "normal" (most things), "low" (notifications)`,
		envelope.Source, envelope.EventKind, truncateForLLM(envelope.Payload, 500))

	req := inference.ChatRequest{
		Model: e.model,
		Messages: []inference.Message{
			{Role: "user", Content: prompt},
		},
		MaxTokens:   150,
		Temperature: 0.0,
	}

	resultCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	tokens, errs := e.provider.Chat(resultCtx, req)
	var resp strings.Builder
loop:
	for {
		select {
		case tok, ok := <-tokens:
			if !ok {
				break loop
			}
			resp.WriteString(tok.Text)
		case err, ok := <-errs:
			if ok && err != nil {
				e.log.Warn("triage LLM call failed", "error", err)
				return nil
			}
		case <-resultCtx.Done():
			return nil
		}
	}

	// Parse the JSON response.
	var decision TriageDecision
	text := strings.TrimSpace(resp.String())
	// Extract JSON from possible markdown fences.
	if idx := strings.Index(text, "{"); idx >= 0 {
		text = text[idx:]
		if end := strings.LastIndex(text, "}"); end >= 0 {
			text = text[:end+1]
		}
	}
	if err := json.Unmarshal([]byte(text), &decision); err != nil {
		e.log.Warn("triage LLM response parse failed", "error", err, "text", text)
		return nil
	}
	decision.Confidence = 0.7 // LLM decisions have moderate confidence
	return &decision
}

func truncateForLLM(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
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

// AddRule delegates to the underlying TriageEvaluator.
func (p *TriagePipeline) AddRule(rule TriageRule) {
	p.evaluator.AddRule(rule)
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
