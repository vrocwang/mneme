package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/cloudwego/eino/compose"

	"github.com/simon/mneme/internal/inference"
)

// ── eino compose.Graph-based triage classification ────────────────────
//
// Replaces the manual 4-step fallback chain (rules → cloud LLM → local AI →
// defer) in TriageClassifier.Classify() with a compiled eino Graph. Each
// classification step is a compose.InvokableLambda node. Shared state carries
// the envelope through all nodes.
//
// Benefits over the imperative fallback:
//   - Visibility via eino's callback system (OnStart/OnEnd per node)
//   - One-time compilation — compiled once, invoked per envelope
//   - Structured error propagation

// ClassificationChainConfig holds the dependencies for the classification
// graph. All fields except Rules are optional — when nil, the corresponding
// step is skipped.
type ClassificationChainConfig struct {
	Rules func(ctx context.Context, env *TriageEnvelope) *TriageDecision

	CloudProvider inference.Provider
	CloudModel    string

	LocalProvider inference.Provider
	LocalModel    string

	// TargetAgents is the roster of routable agent IDs injected into the
	// classifier prompt. When empty, a minimal default is used.
	TargetAgents []string

	Logger *slog.Logger
}

// triageChainState carries the envelope through the graph via shared state.
type triageChainState struct {
	Envelope *TriageEnvelope
}

// NewClassificationGraph builds and compiles the triage classification
// graph. Returns a Runnable that accepts *TriageEnvelope and returns
// *TriageDecision.
func NewClassificationGraph(cfg ClassificationChainConfig) (compose.Runnable[*TriageEnvelope, *TriageDecision], error) {
	if cfg.Rules == nil {
		return nil, fmt.Errorf("triage graph: Rules is required")
	}

	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}

	systemPrompt := triageSystemPromptFor(cfg.TargetAgents)

	g := compose.NewGraph[*TriageEnvelope, *TriageDecision](
		compose.WithGenLocalState(func(ctx context.Context) *triageChainState {
			return &triageChainState{}
		}),
	)

	// Node: store envelope into state, run rules.
	if err := g.AddLambdaNode("rules", compose.InvokableLambda(
		func(ctx context.Context, env *TriageEnvelope) (*TriageDecision, error) {
			_ = compose.ProcessState[*triageChainState](ctx, func(ctx context.Context, s *triageChainState) error {
				s.Envelope = env
				return nil
			})
			return cfg.Rules(ctx, env), nil
		},
	)); err != nil {
		return nil, err
	}

	// Node: cloud LLM classification with built-in retry via ModelRetryConfig
	// pattern. Runs only when rules confidence < 0.8.
	cloudAdded := false
	if cfg.CloudProvider != nil {
		prov := cfg.CloudProvider
		mdl := cfg.CloudModel
		if err := g.AddLambdaNode("cloud_llm", compose.InvokableLambda(
			func(ctx context.Context, prev *TriageDecision) (*TriageDecision, error) {
				if prev.Confidence >= 0.8 {
					return prev, nil // rules already confident
				}
				var env *TriageEnvelope
				_ = compose.ProcessState[*triageChainState](ctx, func(ctx context.Context, s *triageChainState) error {
					env = s.Envelope
					return nil
				})
				if env == nil {
					return prev, nil
				}
				d, err := callLLMForTriage(ctx, env, prov, mdl, systemPrompt, log)
				if err != nil {
					log.Warn("triage graph: cloud LLM failed", "error", err)
					return prev, nil // pass through to next step
				}
				return d, nil
			},
		)); err != nil {
			return nil, err
		}
		cloudAdded = true
	}

	// Node: local AI fallback — runs only when cloud didn't succeed.
	localAdded := false
	if cfg.LocalProvider != nil {
		prov := cfg.LocalProvider
		mdl := cfg.LocalModel
		if err := g.AddLambdaNode("local_ai", compose.InvokableLambda(
			func(ctx context.Context, prev *TriageDecision) (*TriageDecision, error) {
				if prev.Confidence >= 0.7 {
					return prev, nil // previous step succeeded
				}
				var env *TriageEnvelope
				_ = compose.ProcessState[*triageChainState](ctx, func(ctx context.Context, s *triageChainState) error {
					env = s.Envelope
					return nil
				})
				if env == nil {
					return prev, nil
				}
				d, err := callLLMForTriage(ctx, env, prov, mdl, systemPrompt, log)
				if err != nil {
					log.Warn("triage graph: local AI failed", "error", err)
					return prev, nil
				}
				return d, nil
			},
		)); err != nil {
			return nil, err
		}
		localAdded = true
	}

	// Node: ensure we always return a valid decision (defer as ultimate fallback).
	if err := g.AddLambdaNode("ensure", compose.InvokableLambda(
		func(ctx context.Context, prev *TriageDecision) (*TriageDecision, error) {
			if prev != nil && prev.Action != "" {
				return prev, nil
			}
			return &TriageDecision{
				Action:     TriageDefer,
				Priority:   "normal",
				Confidence: 0,
				Reason:     "all classification paths exhausted",
			}, nil
		},
	)); err != nil {
		return nil, err
	}

	// Wire edges sequentially.
	if err := g.AddEdge(compose.START, "rules"); err != nil {
		return nil, err
	}

	prev := "rules"
	if cloudAdded {
		if err := g.AddEdge(prev, "cloud_llm"); err != nil {
			return nil, err
		}
		prev = "cloud_llm"
	}
	if localAdded {
		if err := g.AddEdge(prev, "local_ai"); err != nil {
			return nil, err
		}
		prev = "local_ai"
	}
	if err := g.AddEdge(prev, "ensure"); err != nil {
		return nil, err
	}
	if err := g.AddEdge("ensure", compose.END); err != nil {
		return nil, err
	}

	return g.Compile(context.Background(),
		compose.WithGraphName("triage_classification"),
		compose.WithNodeTriggerMode(compose.AllPredecessor),
	)
}

// callLLMForTriage makes a single LLM call for triage classification.
func callLLMForTriage(ctx context.Context, env *TriageEnvelope, provider inference.Provider, model, systemPrompt string, log *slog.Logger) (*TriageDecision, error) {
	prompt := buildTriagePrompt(env)

	req := inference.ChatRequest{
		Model: model,
		Messages: []inference.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   256,
		Temperature: 0.1,
	}

	resultCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	tokens, errs := provider.Chat(resultCtx, req)

	var text string
	var lastErr error

loop:
	for {
		select {
		case tok, ok := <-tokens:
			if !ok {
				break loop
			}
			text += tok.Text
		case e, ok := <-errs:
			if !ok {
				break loop
			}
			if e != nil {
				lastErr = e
			}
		case <-resultCtx.Done():
			return nil, fmt.Errorf("provider timeout")
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	if text == "" {
		return nil, fmt.Errorf("empty response")
	}

	return parseTriageResponse(text)
}

// triageSystemPrompt instructs the small model to classify events into a
// JSON routing decision. The agent roster is appended per configuration so it
// tracks the live capability registry instead of a frozen copy.
const triageSystemPrompt = `You are a routing classifier. Analyze the incoming event and return a JSON decision.
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
- "escalate": needs human attention (error, security issue, unknown format)`

// defaultTriageTargetAgents is the fallback roster when no registry is wired.
var defaultTriageTargetAgents = []string{"general"}

// triageSystemPromptFor returns the classifier prompt with the routable agent
// roster appended.
func triageSystemPromptFor(agents []string) string {
	if len(agents) == 0 {
		agents = defaultTriageTargetAgents
	}
	return triageSystemPrompt + "\n\nTarget agents: " + strings.Join(agents, ", ")
}

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
