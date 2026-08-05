package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/simon/mneme/internal/inference"
)

// ── Extended trigger sources ─────────────────────────────────────────────

// TriggerSource classifies where a trigger originated.
type TriggerSource string

const (
	SourceComposio           TriggerSource = "composio"
	SourceWebviewIntegration TriggerSource = "webview_integration"
	SourceWebhook            TriggerSource = "webhook"
	SourceCron               TriggerSource = "cron"
	SourceExternal           TriggerSource = "external"
	SourceManual             TriggerSource = "manual"
)

// ── LLM-assisted triage ──────────────────────────────────────────────────

// TriageLLMOptions configures the LLM-assisted triage fallback chain.
type TriageLLMOptions struct {
	// CloudProvider is the primary inference provider (e.g. Anthropic, OpenAI).
	CloudProvider inference.Provider

	// LocalProvider is the fallback for when the cloud is unavailable or budget-exhausted.
	LocalProvider inference.Provider

	// SmallModel is the model ID for fast triage classification.
	SmallModel string

	// DeferRetryAfter is how long to wait before retrying a deferred decision.
	DeferRetryAfter time.Duration

	// MaxCloudRetries limits cloud retries before falling back to local.
	MaxCloudRetries int
}

// DefaultTriageLLMOptions returns sensible defaults.
func DefaultTriageLLMOptions() TriageLLMOptions {
	return TriageLLMOptions{
		SmallModel:      "claude-haiku-4-5-20251001",
		DeferRetryAfter: 30 * time.Second,
		MaxCloudRetries: 2,
	}
}

// ── TriageClassifier with LLM fallback ───────────────────────────────────

// TriageClassifier extends the rule-based evaluator with an LLM fallback chain.
// When static rules don't match with sufficient confidence, it:
// 1. Tries the cloud model (with retry on transient errors)
// 2. Falls back to a local AI model
// 3. Defers for later retry if all paths fail
type TriageClassifier struct {
	*TriageEvaluator
	opts TriageLLMOptions
	log  *slog.Logger
}

// NewTriageClassifier creates a triage classifier with LLM fallback.
func NewTriageClassifier(opts TriageLLMOptions) *TriageClassifier {
	if opts.SmallModel == "" {
		opts.SmallModel = "claude-haiku-4-5-20251001"
	}
	if opts.DeferRetryAfter == 0 {
		opts.DeferRetryAfter = 30 * time.Second
	}
	if opts.MaxCloudRetries <= 0 {
		opts.MaxCloudRetries = 2
	}
	return &TriageClassifier{
		TriageEvaluator: NewTriageEvaluator(),
		opts:            opts,
		log:             slog.Default().With("component", "triage-classifier"),
	}
}

// Classify runs the full classification chain: rules → cloud LLM → local AI → defer.
func (c *TriageClassifier) Classify(ctx context.Context, envelope *TriageEnvelope) *TriageDecision {
	// Step 1: Try static rules first (fast path).
	ruleDecision := c.TriageEvaluator.Evaluate(envelope)
	if ruleDecision.Confidence >= 0.8 {
		return ruleDecision
	}

	// Step 2: Try cloud LLM.
	if c.opts.CloudProvider != nil {
		decision, err := c.classifyWithLLM(ctx, envelope, c.opts.CloudProvider, c.opts.SmallModel, c.opts.MaxCloudRetries)
		if err == nil {
			return decision
		}
		if isBudgetExhausted(err) {
			// Budget exhausted: skip directly to local AI, don't retry.
			c.log.Warn("triage: cloud budget exhausted, falling back to local")
		} else if isSafetyRejection(err) {
			// Prompt injection detected: defer — don't page Sentry.
			return &TriageDecision{
				Action:     TriageDefer,
				Priority:   "low",
				Confidence: 0,
				Reason:     "safety rejection detected — deferring",
			}
		} else {
			c.log.Warn("triage: cloud LLM failed, falling back to local", "error", err)
		}
	}

	// Step 3: Try local AI fallback.
	if c.opts.LocalProvider != nil {
		decision, err := c.classifyWithLLM(ctx, envelope, c.opts.LocalProvider, c.opts.SmallModel, 1)
		if err == nil {
			return decision
		}
		c.log.Warn("triage: local AI failed", "error", err)
	}

	// Step 4: Defer for later retry.
	return &TriageDecision{
		Action:     TriageDefer,
		Priority:   "normal",
		Confidence: 0,
		Reason:     fmt.Sprintf("all classification paths exhausted, retry after %v", c.opts.DeferRetryAfter),
	}
}

// classifyWithLLM sends the envelope to an LLM for classification with retry.
func (c *TriageClassifier) classifyWithLLM(ctx context.Context, envelope *TriageEnvelope, provider inference.Provider, model string, maxRetries int) (*TriageDecision, error) {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s...
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		decision, err := c.callLLM(ctx, envelope, provider, model)
		if err == nil {
			return decision, nil
		}

		if !IsTransientError(err) {
			return nil, err // permanent error, don't retry
		}

		lastErr = err
		c.log.Debug("triage: LLM call failed, retrying", "attempt", attempt+1, "error", err)
	}

	return nil, fmt.Errorf("LLM classification failed after %d attempts: %w", maxRetries, lastErr)
}

// callLLM makes a single LLM call for triage classification.
func (c *TriageClassifier) callLLM(ctx context.Context, envelope *TriageEnvelope, provider inference.Provider, model string) (*TriageDecision, error) {
	prompt := buildTriagePrompt(envelope)

	messages := []inference.Message{
		{Role: "system", Content: triageSystemPrompt},
		{Role: "user", Content: prompt},
	}

	req := inference.ChatRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   256,
		Temperature: 0.1,
	}

	tokens, errs := provider.Chat(ctx, req)

	var text strings.Builder
	var lastErr error

loop:
	for {
		select {
		case tok, ok := <-tokens:
			if !ok {
				break loop
			}
			if tok.Text != "" {
				text.WriteString(tok.Text)
			}
		case e, ok := <-errs:
			if !ok {
				break loop
			}
			if e != nil {
				lastErr = e
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}

	response := text.String()
	if strings.TrimSpace(response) == "" {
		return nil, fmt.Errorf("empty response from provider")
	}

	return parseTriageResponse(response)
}

const triageSystemPrompt = `You are a routing classifier. Analyze the incoming event and return a JSON decision.

Return ONLY valid JSON:
{
  "action": "route" | "drop" | "defer" | "escalate",
  "target_agent": "agent_name" (for route action),
  "priority": "critical" | "high" | "normal" | "low",
  "confidence": 0.0-1.0,
  "reason": "brief explanation"
}

Routing guidelines:
- "route": event matches a known agent capability — include target_agent
- "drop": spam, test events, or irrelevant content
- "defer": cannot classify now, re-queue for later
- "escalate": needs human attention (error, security issue, unknown format)

Target agents: general, researcher, coder, planner, archivist, summarizer, help, morning_briefing, desktop_control, integrations, trigger_reactor`

func buildTriagePrompt(envelope *TriageEnvelope) string {
	// Truncate payload to 2000 chars for classification.
	payload := envelope.Payload
	if len(payload) > 2000 {
		payload = payload[:2000] + "..."
	}

	return fmt.Sprintf(`Classify this event:

Source: %s
Event Kind: %s
Received: %s

Payload:
%s`, envelope.Source, envelope.EventKind, envelope.ReceivedAt.Format(time.RFC3339), payload)
}

// ── Forgiving JSON parser ────────────────────────────────────────────────

var (
	jsonFenceRe       = regexp.MustCompile("(?s)`{3,}\\s*(?:json)?\\s*\n?(.*?)\n?`{3,}")
	trailingCommaRe   = regexp.MustCompile(`,(\s*[}\]])`)
	wrongCaseActionRe = regexp.MustCompile(`"action"\s*:\s*"(\w+)"`)
)

// parseTriageResponse extracts a TriageDecision from LLM output, applying
// forgiving parsing for small-model quirks.
func parseTriageResponse(raw string) (*TriageDecision, error) {
	cleaned := raw

	// 1. Extract from fenced JSON blocks
	if m := jsonFenceRe.FindStringSubmatch(cleaned); len(m) > 1 {
		cleaned = m[1]
	}

	// 2. Trim prose wrapping
	if idx := strings.Index(cleaned, "{"); idx >= 0 {
		cleaned = cleaned[idx:]
	}
	if idx := strings.LastIndex(cleaned, "}"); idx >= 0 {
		cleaned = cleaned[:idx+1]
	}

	// 3. Fix trailing commas
	cleaned = trailingCommaRe.ReplaceAllString(cleaned, "$1")

	// 4. Normalize action case
	cleaned = wrongCaseActionRe.ReplaceAllStringFunc(cleaned, func(match string) string {
		// match is like `"action": "Route"` → `"action": "route"`
		parts := wrongCaseActionRe.FindStringSubmatch(match)
		if len(parts) > 1 {
			lower := strings.ToLower(parts[1])
			return fmt.Sprintf(`"action": "%s"`, lower)
		}
		return match
	})

	var decision TriageDecision
	if err := json.Unmarshal([]byte(cleaned), &decision); err != nil {
		return nil, fmt.Errorf("parse triage response: %w (raw: %.200s)", err, raw)
	}

	// Validate action
	switch decision.Action {
	case TriageRoute, TriageDrop, TriageDefer, TriageEscalate:
		// valid
	default:
		decision.Action = TriageEscalate
		decision.Reason = "unknown action in LLM output: " + string(decision.Action)
	}

	// Clamp confidence
	if decision.Confidence < 0 {
		decision.Confidence = 0
	}
	if decision.Confidence > 1.0 {
		decision.Confidence = 1.0
	}

	return &decision, nil
}

// ── Safety and budget detection ──────────────────────────────────────────

// safetyRejectionMarkers is the set of strings that indicate a safety/prompt-injection rejection.
var safetyRejectionMarkers = []string{
	"prompt injection",
	"prompt injection detected",
	"safety filter",
	"content policy violation",
	"harmful content",
	"blocked by safety",
	"jailbreak",
}

// budgetExhaustedMarkers indicates inference budget exhaustion (not a fatal error).
var budgetExhaustedMarkers = []string{
	"budget exhausted",
	"insufficient funds",
	"credit limit reached",
	"usage limit exceeded",
	"rate limit exceeded",
	"quota exceeded",
	"billing",
}

// isBudgetExhausted returns true if the error indicates an inference budget issue.
func isBudgetExhausted(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, m := range budgetExhaustedMarkers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

// isSafetyRejection returns true if the error indicates a safety/prompt-injection block.
func isSafetyRejection(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, m := range safetyRejectionMarkers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

// ── Approval gate integration ────────────────────────────────────────────

// ApprovalGateChecker is the minimal interface for checking the approval gate
// before dispatching triage decisions. This avoids a circular import with the
// approval package.
type ApprovalGateChecker interface {
	// NeedsApproval returns true if the given action requires user consent.
	NeedsApproval(toolName string, args map[string]interface{}) bool
}

// TriageApprovalPipe wraps a TriagePipeline with approval gate integration.
type TriageApprovalPipe struct {
	*TriagePipeline
	gate ApprovalGateChecker
	log  *slog.Logger
}

// NewTriageApprovalPipe creates an approval-aware triage pipeline.
func NewTriageApprovalPipe(pipeline *TriagePipeline, gate ApprovalGateChecker) *TriageApprovalPipe {
	return &TriageApprovalPipe{
		TriagePipeline: pipeline,
		gate:           gate,
		log:            slog.Default().With("component", "triage-approval"),
	}
}

// ProcessWithApproval evaluates and dispatches, checking the approval gate
// before routing to an agent. If the gate requires consent, the decision is
// parked as "defer" with a note rather than being dispatched.
func (p *TriageApprovalPipe) ProcessWithApproval(ctx context.Context, envelope *TriageEnvelope) (*TriageDecision, error) {
	decision := p.evaluator.Evaluate(envelope)

	if decision.Action == TriageRoute && p.gate != nil {
		// Check if dispatching this agent requires approval.
		if p.gate.NeedsApproval(decision.TargetAgent, map[string]interface{}{
			"source":   envelope.Source,
			"event":    envelope.EventKind,
			"priority": decision.Priority,
		}) {
			p.log.Info("triage: routing deferred for approval",
				"target", decision.TargetAgent,
				"source", envelope.Source,
			)
			return &TriageDecision{
				Action:      TriageDefer,
				TargetAgent: decision.TargetAgent,
				Priority:    decision.Priority,
				Confidence:  decision.Confidence,
				Reason:      "awaiting approval consent",
			}, nil
		}
	}

	return p.TriagePipeline.Process(ctx, envelope)
}

// ── Deferred retry queue ─────────────────────────────────────────────────

// DeferredEnvelope is an envelope queued for later retry.
type DeferredEnvelope struct {
	Envelope    *TriageEnvelope `json:"envelope"`
	RetryAt     time.Time       `json:"retry_at"`
	Attempts    int             `json:"attempts"`
	MaxAttempts int             `json:"max_attempts"`
}

// DeferredQueue manages retry-able triage envelopes.
type DeferredQueue struct {
	items   []DeferredEnvelope
	maxSize int
}

// NewDeferredQueue creates a deferred triage queue.
func NewDeferredQueue(maxSize int) *DeferredQueue {
	if maxSize <= 0 {
		maxSize = 100
	}
	return &DeferredQueue{maxSize: maxSize}
}

// Push adds an envelope for later retry.
func (q *DeferredQueue) Push(env *DeferredEnvelope) {
	if len(q.items) >= q.maxSize {
		// Drop oldest.
		q.items = q.items[1:]
	}
	q.items = append(q.items, *env)
}

// PopReady returns all envelopes whose retry time has elapsed.
func (q *DeferredQueue) PopReady() []DeferredEnvelope {
	var ready []DeferredEnvelope
	now := time.Now()
	remaining := make([]DeferredEnvelope, 0, len(q.items))
	for _, env := range q.items {
		if now.After(env.RetryAt) {
			ready = append(ready, env)
		} else {
			remaining = append(remaining, env)
		}
	}
	q.items = remaining
	return ready
}

// Len returns the queue length.
func (q *DeferredQueue) Len() int {
	return len(q.items)
}
