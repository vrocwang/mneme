package agent

import (
	"github.com/simon/mneme/internal/inference"
)

// TokenBudget controls how many tokens an agent turn may consume.
type TokenBudget struct {
	// MaxInputTokens is the context window size of the model (e.g. 200000 for Claude).
	MaxInputTokens int
	// OutputReserveTokens is the number of tokens reserved for the model's response.
	OutputReserveTokens int
	// SystemPromptTokens is the estimated token count of the system prompt.
	SystemPromptTokens int
}

const (
	// DefaultMaxInputTokens is a safe default for modern models.
	DefaultMaxInputTokens = 100000
	// DefaultOutputReserveTokens reserves room for the model's response.
	DefaultOutputReserveTokens = 8192
	// MinTokensToKeep prevents trimming all the way to zero.
	MinTokensToKeep = 500
)

// DefaultTokenBudget returns a conservative budget for modern models.
func DefaultTokenBudget() *TokenBudget {
	return &TokenBudget{
		MaxInputTokens:      DefaultMaxInputTokens,
		OutputReserveTokens: DefaultOutputReserveTokens,
	}
}

// Available returns how many tokens are left for conversation history.
func (b *TokenBudget) Available() int {
	available := b.MaxInputTokens - b.OutputReserveTokens - b.SystemPromptTokens
	if available < MinTokensToKeep {
		return MinTokensToKeep
	}
	return available
}

// SetSystemPrompt estimates token count for the system prompt.
func (b *TokenBudget) SetSystemPrompt(prompt string) {
	b.SystemPromptTokens = EstimateTokens(prompt)
}

// TrimMessagesToBudget trims messages to fit within the token budget.
// System messages are preserved. Oldest non-system messages are trimmed first.
// Orphaned tool results (without a preceding tool call) are also trimmed to avoid
// provider 400 errors from incomplete tool-call/result pairs.
func (b *TokenBudget) TrimMessagesToBudget(messages []inference.Message) []inference.Message {
	budget := b.Available()
	if budget <= 0 {
		budget = MinTokensToKeep
	}

	totalTokens := EstimateMessagesTokens(messages)
	if totalTokens <= budget {
		return messages
	}

	// Work backwards: keep the most recent messages that fit the budget.
	// Also ensure we don't leave orphaned tool results without their tool calls.
	kept := make([]inference.Message, 0, len(messages))
	usedTokens := 0

	// Track whether we've seen a tool call for each pending tool result.
	pendingToolIDs := make(map[string]bool)

	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		tokEst := len([]rune(msg.Content)) / 4
		if tokEst < 1 {
			tokEst = 1
		}

		// Don't trim system messages.
		if msg.Role == "system" {
			kept = append(kept, msg)
			continue
		}

		// Track orphan tool results.
		if msg.Role == "tool" && msg.ToolID != "" {
			if usedTokens+tokEst > budget {
				continue // can't fit this or previous messages
			}
			pendingToolIDs[msg.ToolID] = true
			kept = append(kept, msg)
			usedTokens += tokEst
			continue
		}

		// If this is an assistant message with a tool call, ensure any orphaned
		// tool results from this message are also kept.
		if msg.Role == "assistant" && msg.ToolCall != nil {
			if pendingToolIDs[msg.ToolCall.ID] {
				// Tool result was already kept, keep this tool call too.
				if usedTokens+tokEst > budget {
					// Budget exceeded — drop this pair.
					delete(pendingToolIDs, msg.ToolCall.ID)
					continue
				}
				delete(pendingToolIDs, msg.ToolCall.ID)
				kept = append(kept, msg)
				usedTokens += tokEst
				continue
			}
		}

		// Normal message: keep if within budget.
		if usedTokens+tokEst > budget {
			continue
		}
		kept = append(kept, msg)
		usedTokens += tokEst
	}

	// Reverse to restore chronological order.
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}

	return kept
}

// IsNearLimit checks whether the total tokens are approaching the limit.
func (b *TokenBudget) IsNearLimit(messages []inference.Message) bool {
	total := EstimateMessagesTokens(messages) + b.SystemPromptTokens
	limit := b.MaxInputTokens - b.OutputReserveTokens
	return total > int(float64(limit)*0.85)
}

// Utilization returns the percentage (0-100) of the budget consumed.
func (b *TokenBudget) Utilization(messages []inference.Message) float64 {
	total := EstimateMessagesTokens(messages) + b.SystemPromptTokens
	limit := b.MaxInputTokens - b.OutputReserveTokens
	if limit <= 0 {
		return 100
	}
	return float64(total) / float64(limit) * 100
}

// ContextGuardResult is the result of a context guard check.
type ContextGuardResult int

const (
	// GuardOK means the context is within budget.
	GuardOK ContextGuardResult = iota
	// GuardCompactionNeeded means the context should be compacted soon.
	GuardCompactionNeeded
	// GuardExhausted means the context is exhausted and must be trimmed.
	GuardExhausted
)

// CheckGuard evaluates whether the context needs attention.
func (b *TokenBudget) CheckGuard(messages []inference.Message) ContextGuardResult {
	util := b.Utilization(messages)
	switch {
	case util > 95:
		return GuardExhausted
	case util > 80:
		return GuardCompactionNeeded
	default:
		return GuardOK
	}
}

// CompactHistory applies trimming and returns both the trimmed messages and
// a summary of what was trimmed (for logging/debugging).
func (b *TokenBudget) CompactHistory(messages []inference.Message) ([]inference.Message, int) {
	before := EstimateMessagesTokens(messages)
	trimmed := b.TrimMessagesToBudget(messages)
	after := EstimateMessagesTokens(trimmed)
	dropped := len(messages) - len(trimmed)
	_ = before - after // token savings for logging
	return trimmed, dropped
}
