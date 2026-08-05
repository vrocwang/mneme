package agent

import (
	"context"
	"fmt"
	"log/slog"
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
				d, err := callLLMForTriage(ctx, env, prov, mdl, log)
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
				d, err := callLLMForTriage(ctx, env, prov, mdl, log)
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
func callLLMForTriage(ctx context.Context, env *TriageEnvelope, provider inference.Provider, model string, log *slog.Logger) (*TriageDecision, error) {
	prompt := buildTriagePrompt(env)

	req := inference.ChatRequest{
		Model: model,
		Messages: []inference.Message{
			{Role: "system", Content: triageSystemPrompt},
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
