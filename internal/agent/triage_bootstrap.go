package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/inference"
)

// WebhookChatSender is the interface for sending messages to the agent chat
// service from webhook triage. Satisfied by ChatService.Send.
type WebhookChatSender interface {
	Send(ctx context.Context, threadID, message string) (string, error)
}

// NewDefaultTriagePipeline creates a triage pipeline with sensible defaults
// and a real task dispatcher that routes webhook tasks to the chat service.
func NewDefaultTriagePipeline(capReg *capability.CapabilityRegistry, sender WebhookChatSender) *TriagePipeline {
	eval := NewTriageEvaluator()

	executor := TaskExecutor(func(ctx context.Context, task *DispatchTask) error {
		if capReg != nil {
			if _, ok := capReg.GetAgent(task.AgentID); !ok {
				slog.Warn("triage dispatch: agent not found",
					"task", task.ID, "agent", task.AgentID)
			}
		}
		if sender != nil {
			threadID := "webhook:" + task.ID
			response, err := sender.Send(ctx, threadID, task.Prompt)
			if err != nil {
				return fmt.Errorf("webhook agent execution failed: %w", err)
			}
			slog.Info("triage task executed",
				"id", task.ID, "agent", task.AgentID,
				"priority", task.Priority, "response_len", len(response))
		} else {
			slog.Info("triage task dispatched (no sender configured)",
				"id", task.ID, "agent", task.AgentID,
				"priority", task.Priority, "prompt_len", len(task.Prompt))
		}
		return nil
	})

	gate := TaskApprovalGate(func(task *DispatchTask) (bool, string) {
		return true, "auto-approved"
	})

	disp := NewTaskDispatcher(executor, gate)
	go disp.Start(context.Background())

	slog.Info("triage pipeline initialized with dispatcher")
	return NewTriagePipeline(eval, disp)
}

// NewGraphTriagePipeline creates a triage pipeline that uses the compiled
// eino classification graph instead of the imperative TriageClassifier.
// When cloudProvider is nil, the graph skips the LLM step and falls through
// rules only (same behavior as a plain TriageEvaluator).
func NewGraphTriagePipeline(
	capReg *capability.CapabilityRegistry,
	sender WebhookChatSender,
	cloudProvider inference.Provider,
	cloudModel string,
) (*TriagePipeline, error) {
	// Create an evaluator for rules-only matching (no LLM wired — the graph
	// handles LLM fallback in its own nodes).
	eval := NewTriageEvaluator()

	// Build the compiled eino classification graph.
	graph, err := NewClassificationGraph(ClassificationChainConfig{
		Rules: func(ctx context.Context, env *TriageEnvelope) *TriageDecision {
			return eval.Evaluate(env)
		},
		CloudProvider: cloudProvider,
		CloudModel:    cloudModel,
		Logger:        slog.Default().With("component", "triage-graph"),
	})
	if err != nil {
		return nil, fmt.Errorf("triage graph: compile: %w", err)
	}

	// Same executor and dispatcher as the default pipeline.
	executor := TaskExecutor(func(ctx context.Context, task *DispatchTask) error {
		if capReg != nil {
			if _, ok := capReg.GetAgent(task.AgentID); !ok {
				slog.Warn("triage dispatch: agent not found",
					"task", task.ID, "agent", task.AgentID)
			}
		}
		if sender != nil {
			threadID := "webhook:" + task.ID
			response, err := sender.Send(ctx, threadID, task.Prompt)
			if err != nil {
				return fmt.Errorf("webhook agent execution failed: %w", err)
			}
			slog.Info("triage task executed",
				"id", task.ID, "agent", task.AgentID,
				"priority", task.Priority, "response_len", len(response))
		} else {
			slog.Info("triage task dispatched (no sender configured)",
				"id", task.ID, "agent", task.AgentID,
				"priority", task.Priority, "prompt_len", len(task.Prompt))
		}
		return nil
	})

	gate := TaskApprovalGate(func(task *DispatchTask) (bool, string) {
		return true, "auto-approved"
	})

	disp := NewTaskDispatcher(executor, gate)
	go disp.Start(context.Background())

	pipeline := NewTriagePipeline(eval, disp)
	pipeline.WithGraphClassifier(func(ctx context.Context, input *TriageEnvelope) (*TriageDecision, error) {
		return graph.Invoke(ctx, input)
	})

	slog.Info("triage pipeline initialized with eino graph classifier",
		"cloud_model", cloudModel)
	return pipeline, nil
}

// EnqueueWebhookTask creates a dispatch task from a triage decision and
// enqueues it in the dispatcher.
func EnqueueWebhookTask(disp *TaskDispatcher, decision *TriageDecision, envelope *TriageEnvelope) error {
	if decision.Action != TriageRoute {
		return nil
	}

	task := &DispatchTask{
		ID:          uuid.New().String(),
		AgentID:     decision.TargetAgent,
		Prompt:      envelope.Payload,
		Priority:    decision.Priority,
		Status:      "pending",
		MaxRetries:  1,
		ScheduledAt: time.Now().UTC(),
		CreatedAt:   time.Now().UTC(),
	}

	return disp.Enqueue(task)
}
