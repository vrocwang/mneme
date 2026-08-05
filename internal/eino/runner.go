// Package eino provides an adapter layer that maps Mneme config to
// cloudwego/eino chat model instances and agent definitions.
package eino

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/simon/mneme/internal/agent"
	"github.com/simon/mneme/internal/eino/callbacks"
	"github.com/simon/mneme/internal/eino/middleware"
)

// Runner wraps an adk.Runner with the retained subsystems (memory pipeline,
// security gate, circuit breaker, audit/cost/learning callbacks). It is the
// primary entry point for executing agent turns through the eino pipeline.
type Runner struct {
	adkRunner *adk.Runner
	agentSet  *AgentSet
	callbacks *callbacks.Manager
	memMW     *middleware.MemoryMiddleware
	secMW     *middleware.SecurityMiddleware
	breakerMW *middleware.CircuitBreakerMiddleware
	log       *slog.Logger
}

// RunnerConfig holds the dependencies needed to create a Runner.
type RunnerConfig struct {
	AgentSet   *AgentSet
	Callbacks  *callbacks.Manager
	MemoryMW   *middleware.MemoryMiddleware
	SecurityMW *middleware.SecurityMiddleware
	BreakerMW  *middleware.CircuitBreakerMiddleware
	Log        *slog.Logger

	// CheckPointStore, when set, enables automatic checkpoint persistence
	// via eino. Long-running tasks can be paused and resumed from the
	// stored checkpoint.
	CheckPointStore adk.CheckPointStore
}

// NewRunner creates a Runner that wraps the adk.Runner. The general agent
// from the AgentSet is used as the top-level agent, with streaming enabled.
func NewRunner(ctx context.Context, cfg RunnerConfig) (*Runner, error) {
	if cfg.AgentSet == nil {
		return nil, fmt.Errorf("eino: RunnerConfig.AgentSet is nil")
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}

	adkCfg := adk.RunnerConfig{
		Agent:           cfg.AgentSet.General,
		EnableStreaming: true,
		CheckPointStore: cfg.CheckPointStore,
	}
	adkRunner := adk.NewRunner(ctx, adkCfg)

	return &Runner{
		adkRunner: adkRunner,
		agentSet:  cfg.AgentSet,
		callbacks: cfg.Callbacks,
		memMW:     cfg.MemoryMW,
		secMW:     cfg.SecurityMW,
		breakerMW: cfg.BreakerMW,
		log:       log,
	}, nil
}

// Query runs a single-turn query through the eino agent pipeline. It:
//  1. Runs a security check on the user input
//  2. Delegates to the adk.Runner for agent execution
//  3. Iterates agent events, collecting the final response and tool call
//     results
//  4. Runs post-turn memory extraction and learning reflection
//  5. Returns a TurnResult compatible with the existing ChatService
//     contract
func (r *Runner) Query(ctx context.Context, threadID, userMessage string) (*agent.TurnResult, error) {
	// -- 1. Security check ----------------------------------------------
	if r.secMW != nil {
		if err := r.secMW.FilterInput(ctx, userMessage); err != nil {
			r.log.Warn("eino runner: input blocked by security", "err", err, "thread", threadID)
			return &agent.TurnResult{
				ThreadID: threadID,
				Response: "Your message was blocked by security policy.",
				Error:    err,
			}, nil
		}
	}

	// -- 2. Run the agent via adk.Runner --------------------------------
	iter := r.adkRunner.Query(ctx, userMessage)
	return r.processEvents(ctx, threadID, userMessage, iter)
}

// QueryWithCheckpoint runs a single-turn query with a checkpoint ID,
// enabling pause/resume via the configured CheckPointStore. The checkpoint
// is automatically saved by eino at each agent execution step.
func (r *Runner) QueryWithCheckpoint(ctx context.Context, threadID, checkPointID, userMessage string) (*agent.TurnResult, error) {
	// -- 1. Security check ----------------------------------------------
	if r.secMW != nil {
		if err := r.secMW.FilterInput(ctx, userMessage); err != nil {
			r.log.Warn("eino runner: input blocked by security", "err", err, "thread", threadID)
			return &agent.TurnResult{
				ThreadID: threadID,
				Response: "Your message was blocked by security policy.",
				Error:    err,
			}, nil
		}
	}

	// -- 2. Run the agent with checkpoint ID ----------------------------
	iter := r.adkRunner.Query(ctx, userMessage, adk.WithCheckPointID(checkPointID))
	return r.processEvents(ctx, threadID, userMessage, iter)
}

// processEvents consumes the agent event iterator and builds a TurnResult.
func (r *Runner) processEvents(ctx context.Context, threadID, userMessage string, iter *adk.AsyncIterator[*adk.AgentEvent]) (*agent.TurnResult, error) {
	// -- 3. Process agent events ----------------------------------------
	var finalContent string
	var toolCalls []agent.ToolCallResult
	var inputTokens, outputTokens int

	for {
		event, ok := iter.Next()
		if !ok {
			break // iterator closed, no more events
		}

		// Check for error in the event.
		if event.Err != nil {
			r.log.Warn("eino runner: agent event error", "agent", event.AgentName, "err", event.Err)
			// Non-fatal: continue processing remaining events.
		}

		// Check for exit action.
		if event.Action != nil && event.Action.Exit {
			break
		}

		// Check circuit breaker state.
		if r.breakerMW != nil && r.breakerMW.IsTripped() {
			reason := r.breakerMW.Reason()
			r.log.Warn("eino runner: circuit breaker tripped", "reason", reason, "thread", threadID)
			return &agent.TurnResult{
				ThreadID: threadID,
				Response: "Agent loop was interrupted: " + reason,
				Error:    fmt.Errorf("circuit breaker tripped: %s", reason),
			}, nil
		}

		// Skip events without message output.
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}

		mv := event.Output.MessageOutput
		msg, err := mv.GetMessage()
		if err != nil {
			r.log.Warn("eino runner: failed to get message from event", "err", err)
			continue
		}

		switch mv.Role {
		case schema.Assistant:
			// Model output -- accumulate final text content.
			if msg.Content != "" {
				finalContent = msg.Content
			}
			// Check for narration loops.
			if r.breakerMW != nil {
				r.breakerMW.RecordOutput(msg.Content)
			}
			// Collect tool call requests from the assistant.
			for _, tc := range msg.ToolCalls {
				tcResult := agent.ToolCallResult{
					Name: tc.Function.Name,
				}
				// Parse JSON arguments into the Args map.
				if tc.Function.Arguments != "" {
					var args map[string]interface{}
					if json.Unmarshal([]byte(tc.Function.Arguments), &args) == nil {
						tcResult.Args = args
					}
				}
				toolCalls = append(toolCalls, tcResult)
			}
			// Capture usage from response metadata.
			if msg.ResponseMeta != nil && msg.ResponseMeta.Usage != nil {
				inputTokens = msg.ResponseMeta.Usage.PromptTokens
				outputTokens = msg.ResponseMeta.Usage.CompletionTokens
			}

		case schema.Tool:
			// Tool execution result -- find the matching tool call and
			// attach the output. We match by scanning backwards for the
			// most recent tool call with the same name that has no output.
			if r.breakerMW != nil {
				r.breakerMW.RecordSuccess()
			}
			for i := len(toolCalls) - 1; i >= 0; i-- {
				if toolCalls[i].Name == mv.ToolName && toolCalls[i].Output == "" {
					toolCalls[i].Output = msg.Content
					break
				}
			}
		}
	}

	// -- 4. Post-turn: audit logging ------------------------------------
	if r.callbacks != nil && r.callbacks.Audit != nil {
		r.callbacks.Audit.OnModelCall("default", threadID)
		for _, tc := range toolCalls {
			r.callbacks.Audit.OnToolCall(tc.Name, tc.Output, threadID)
		}
	}

	// -- 5. Post-turn: cost tracking ------------------------------------
	if r.callbacks != nil && r.callbacks.Cost != nil {
		r.callbacks.Cost.OnTokens("default", inputTokens, outputTokens, 0)
	}

	// -- 6. Post-turn: memory extraction --------------------------------
	if r.memMW != nil {
		r.memMW.OnTurnEnd(ctx, threadID, inputTokens+outputTokens, len(toolCalls))
	}

	// -- 7. Post-turn: learning reflection ------------------------------
	if r.callbacks != nil && r.callbacks.Learning != nil {
		r.callbacks.Learning.OnTurnEnd(ctx, threadID, userMessage, finalContent)
	}

	return &agent.TurnResult{
		ThreadID:     threadID,
		Response:     finalContent,
		ToolCalls:    toolCalls,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}, nil
}

// StreamQuery runs a single-turn query through the eino agent pipeline and
// emits streaming events via the onEvent callback. It follows the same
// security / execution / post-turn flow as Query, but yields incremental
// token, tool_call, and tool_result events instead of blocking until
// completion.
//
// The returned TurnResult contains the complete turn data (tool calls, token
// counts) for post-turn persistence and metrics. The "done" streaming event
// is emitted before the TurnResult is returned.
func (r *Runner) StreamQuery(ctx context.Context, threadID, userMessage string, onEvent func(agent.StreamEvent)) (*agent.TurnResult, error) {
	// Global timeout for the entire agent turn (LLM calls + tool executions).
	// Prevents indefinite hangs from interactive commands or slow APIs.
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	ctx = timeoutCtx

	// -- 1. Security check ----------------------------------------------
	if r.secMW != nil {
		if err := r.secMW.FilterInput(ctx, userMessage); err != nil {
			r.log.Warn("eino runner: input blocked by security", "err", err, "thread", threadID)
			onEvent(agent.StreamEvent{ThreadID: threadID, Type: "error", Content: "Your message was blocked by security policy.", Done: true})
			return &agent.TurnResult{
				ThreadID: threadID,
				Response: "Your message was blocked by security policy.",
				Error:    err,
			}, nil
		}
	}

	// -- 2. Run the agent via adk.Runner --------------------------------
	// Pass threadID as checkPointID so adk loads conversation history
	// from CheckPointStore, giving the agent context from prior turns.
	iter := r.adkRunner.Query(ctx, userMessage, adk.WithCheckPointID(threadID))

	// -- 3. Process agent events and emit streaming events --------------
	var finalContent string
	var toolCallResults []agent.ToolCallResult
	var inputTokens, outputTokens int
	var pendingToolCalls []agent.ToolCallResult

	for {
		event, ok := iter.Next()
		if !ok {
			break // iterator closed
		}

		if event.Err != nil {
			errStr := event.Err.Error()
			// Tool execution failures (exit status, command failed) are non-fatal.
			// The agent should see the error output and continue its reasoning.
			if strings.Contains(errStr, "exit status") || strings.Contains(errStr, "command failed") {
				r.log.Debug("eino runner: tool execution failed (non-fatal)", "agent", event.AgentName, "err", event.Err)
				continue
			}
			// Fatal errors (network, auth, model) abort the conversation.
			r.log.Warn("eino runner: agent event error", "agent", event.AgentName, "err", event.Err)
			onEvent(agent.StreamEvent{ThreadID: threadID, Type: "error", Content: "Agent error: " + errStr, Done: true})
			return &agent.TurnResult{
				ThreadID: threadID,
				Response: "Agent error: " + errStr,
				Error:    event.Err,
			}, event.Err
		}

		if event.Action != nil && event.Action.Exit {
			break
		}

		if r.breakerMW != nil && r.breakerMW.IsTripped() {
			reason := r.breakerMW.Reason()
			r.log.Warn("eino runner: circuit breaker tripped during stream", "reason", reason, "thread", threadID)
			onEvent(agent.StreamEvent{ThreadID: threadID, Type: "error", Content: "Agent loop was interrupted: " + reason, Done: true})
			return &agent.TurnResult{
				ThreadID: threadID,
				Response: "Agent loop was interrupted: " + reason,
				Error:    fmt.Errorf("circuit breaker tripped: %s", reason),
			}, nil
		}

		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}

		mv := event.Output.MessageOutput

		switch mv.Role {
		case schema.Assistant:
			var streamToolCalls []schema.ToolCall
			if mv.IsStreaming && mv.MessageStream != nil {
				// Streaming: consume chunks and emit token events.
				finalContent, streamToolCalls = r.consumeStream(threadID, mv.MessageStream, onEvent)
			} else if mv.Message != nil {
				// Non-streaming: emit the full content as a single token event.
				if mv.Message.Content != "" {
					finalContent = mv.Message.Content
					onEvent(agent.StreamEvent{ThreadID: threadID, Type: "token", Content: finalContent, Done: false})
				}
			}

			// Collect tool calls: prefer mv.Message (complete), fallback to stream.
			var toolCallsToEmit []schema.ToolCall
			if mv.Message != nil {
				toolCallsToEmit = mv.Message.ToolCalls
			} else {
				toolCallsToEmit = streamToolCalls
			}
			for _, tc := range toolCallsToEmit {
				tcr := agent.ToolCallResult{
					Name: tc.Function.Name,
				}
				if tc.Function.Arguments != "" {
					var args map[string]interface{}
					if json.Unmarshal([]byte(tc.Function.Arguments), &args) == nil {
						tcr.Args = args
					}
				}
				pendingToolCalls = append(pendingToolCalls, tcr)
				argsJSON := tc.Function.Arguments
				if argsJSON == "" {
					argsJSON = "{}"
				}
				onEvent(agent.StreamEvent{ThreadID: threadID, Type: "tool_call", Content: tc.Function.Name, Args: argsJSON, Done: false})
			}
			toolCallResults = append(toolCallResults, pendingToolCalls...)
			pendingToolCalls = nil

			// Narration loop detection.
			if r.breakerMW != nil && mv.Message != nil {
				r.breakerMW.RecordOutput(mv.Message.Content)
			}
			// Capture usage.
			if mv.Message != nil && mv.Message.ResponseMeta != nil && mv.Message.ResponseMeta.Usage != nil {
				inputTokens = mv.Message.ResponseMeta.Usage.PromptTokens
				outputTokens = mv.Message.ResponseMeta.Usage.CompletionTokens
			}

		case schema.Tool:
			if r.breakerMW != nil {
				r.breakerMW.RecordSuccess()
			}
			// Tool execution result.
			content := ""
			if mv.Message != nil {
				content = mv.Message.Content
			}

			toolName := mv.ToolName
			for i := len(toolCallResults) - 1; i >= 0; i-- {
				if toolCallResults[i].Name == toolName && toolCallResults[i].Output == "" {
					toolCallResults[i].Output = content
					break
				}
			}
			onEvent(agent.StreamEvent{ThreadID: threadID, Type: "tool_result", Content: content, Done: false})
		}
	}

	// -- 4. Post-turn: audit logging ------------------------------------
	if r.callbacks != nil && r.callbacks.Audit != nil {
		r.callbacks.Audit.OnModelCall("default", threadID)
		for _, tc := range toolCallResults {
			r.callbacks.Audit.OnToolCall(tc.Name, tc.Output, threadID)
		}
	}

	// -- 5. Post-turn: cost tracking ------------------------------------
	if r.callbacks != nil && r.callbacks.Cost != nil {
		r.callbacks.Cost.OnTokens("default", inputTokens, outputTokens, 0)
	}

	// -- 6. Post-turn: memory extraction --------------------------------
	if r.memMW != nil {
		r.memMW.OnTurnEnd(ctx, threadID, inputTokens+outputTokens, len(toolCallResults))
	}

	// -- 7. Post-turn: learning reflection ------------------------------
	if r.callbacks != nil && r.callbacks.Learning != nil {
		r.callbacks.Learning.OnTurnEnd(ctx, threadID, userMessage, finalContent)
	}

	// -- 8. Emit done event ---------------------------------------------
	onEvent(agent.StreamEvent{ThreadID: threadID, Type: "done", Content: finalContent, Done: true})

	return &agent.TurnResult{
		ThreadID:     threadID,
		Response:     finalContent,
		ToolCalls:    toolCallResults,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}, nil
}

// consumeStream drains a streaming schema.StreamReader[*schema.Message] and
// emits token events for each content chunk. Returns the concatenated content
// and the latest ToolCalls (caller is responsible for emitting tool_call events).
func (r *Runner) consumeStream(threadID string, stream *schema.StreamReader[*schema.Message], onEvent func(agent.StreamEvent)) (string, []schema.ToolCall) {
	if stream == nil {
		return "", nil
	}
	defer stream.Close()

	var fullContent string
	var accumulatedToolCalls []schema.ToolCall
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			r.log.Warn("eino runner: stream recv error", "err", err)
			break
		}
		if chunk == nil {
			continue
		}
		if chunk.Content != "" {
			fullContent += chunk.Content
			onEvent(agent.StreamEvent{ThreadID: threadID, Type: "token", Content: chunk.Content, Done: false})
		}
		// Accumulate tool calls across chunks. Name may arrive in the first
		// chunk, Arguments may arrive incrementally. Merge non-empty fields.
		for i, tc := range chunk.ToolCalls {
			for len(accumulatedToolCalls) <= i {
				accumulatedToolCalls = append(accumulatedToolCalls, schema.ToolCall{})
			}
			if tc.Function.Name != "" {
				accumulatedToolCalls[i].Function.Name = tc.Function.Name
			}
			// Arguments are incremental deltas (OpenAI streaming) - always append.
			if tc.Function.Arguments != "" {
				accumulatedToolCalls[i].Function.Arguments += tc.Function.Arguments
			}
		}
	}
	return fullContent, accumulatedToolCalls
}

// Resume continues an interrupted agent execution from a checkpoint.
// It loads the saved state via the CheckPointStore and runs the agent
// from where it left off. Post-turn hooks (audit, cost, memory, learning)
// are applied to the resumed turn's output.
//
// The threadID is used for post-turn hook attribution, not for checkpoint
// lookup — the checkPointID is the key used to load the checkpoint.
func (r *Runner) Resume(ctx context.Context, threadID, checkPointID string) (*agent.TurnResult, error) {
	// Apply security filter before resuming (consistent with Query/StreamQuery).
	if r.secMW != nil {
		if err := r.secMW.FilterResume(ctx, checkPointID, threadID); err != nil {
			r.log.Warn("eino runner: resume blocked by security", "err", err, "thread", threadID)
			return &agent.TurnResult{
				ThreadID: threadID,
				Response: "Resume was blocked by security policy.",
				Error:    err,
			}, nil
		}
	}

	iter, err := r.adkRunner.Resume(ctx, checkPointID)
	if err != nil {
		return nil, fmt.Errorf("eino runner: resume %q: %w", checkPointID, err)
	}

	// Process agent events — same pattern as Query.
	var finalContent string
	var toolCalls []agent.ToolCallResult
	var inputTokens, outputTokens int

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}

		if event.Err != nil {
			r.log.Warn("eino runner: resume event error", "agent", event.AgentName, "err", event.Err)
		}

		if event.Action != nil && event.Action.Exit {
			break
		}

		if r.breakerMW != nil && r.breakerMW.IsTripped() {
			reason := r.breakerMW.Reason()
			r.log.Warn("eino runner: circuit breaker tripped during resume", "reason", reason, "thread", threadID)
			return &agent.TurnResult{
				ThreadID: threadID,
				Response: "Agent loop was interrupted: " + reason,
				Error:    fmt.Errorf("circuit breaker tripped: %s", reason),
			}, nil
		}

		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}

		mv := event.Output.MessageOutput
		msg, err := mv.GetMessage()
		if err != nil {
			r.log.Warn("eino runner: failed to get message from resume event", "err", err)
			continue
		}

		switch mv.Role {
		case schema.Assistant:
			if msg.Content != "" {
				finalContent = msg.Content
			}
			if r.breakerMW != nil {
				r.breakerMW.RecordOutput(msg.Content)
			}
			for _, tc := range msg.ToolCalls {
				tcResult := agent.ToolCallResult{Name: tc.Function.Name}
				if tc.Function.Arguments != "" {
					var args map[string]interface{}
					if json.Unmarshal([]byte(tc.Function.Arguments), &args) == nil {
						tcResult.Args = args
					}
				}
				toolCalls = append(toolCalls, tcResult)
			}
			if msg.ResponseMeta != nil && msg.ResponseMeta.Usage != nil {
				inputTokens = msg.ResponseMeta.Usage.PromptTokens
				outputTokens = msg.ResponseMeta.Usage.CompletionTokens
			}

		case schema.Tool:
			if r.breakerMW != nil {
				r.breakerMW.RecordSuccess()
			}
			for i := len(toolCalls) - 1; i >= 0; i-- {
				if toolCalls[i].Name == mv.ToolName && toolCalls[i].Output == "" {
					toolCalls[i].Output = msg.Content
					break
				}
			}
		}
	}

	// Post-turn hooks.
	if r.callbacks != nil && r.callbacks.Audit != nil {
		r.callbacks.Audit.OnModelCall("default", threadID)
		for _, tc := range toolCalls {
			r.callbacks.Audit.OnToolCall(tc.Name, tc.Output, threadID)
		}
	}
	if r.callbacks != nil && r.callbacks.Cost != nil {
		r.callbacks.Cost.OnTokens("default", inputTokens, outputTokens, 0)
	}
	if r.memMW != nil {
		r.memMW.OnTurnEnd(ctx, threadID, inputTokens+outputTokens, len(toolCalls))
	}
	if r.callbacks != nil && r.callbacks.Learning != nil {
		r.callbacks.Learning.OnTurnEnd(ctx, threadID, "", finalContent)
	}

	return &agent.TurnResult{
		ThreadID:     threadID,
		Response:     finalContent,
		ToolCalls:    toolCalls,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}, nil
}

// AgentSet returns the underlying agent set. This is useful for introspection
// and testing.
func (r *Runner) AgentSet() *AgentSet {
	return r.agentSet
}

// ResetBreaker resets the circuit breaker. Call this when the conversation
// context changes significantly or the user acknowledges a detected loop and
// wants to continue.
func (r *Runner) ResetBreaker() {
	if r.breakerMW != nil {
		r.breakerMW.Reset()
	}
}
