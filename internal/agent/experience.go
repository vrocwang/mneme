package agent

import (
	"context"
	"reflect"
	"strings"
)

// Experience represents a learned insight from a prior interaction.
type Experience struct {
	ID       string
	ThreadID string
	Message  string // the user message that triggered this
	Learning string // what was learned
	Context  string // additional context
	Score    float64
}

// ExperienceStore persists and retrieves learned experiences.
type ExperienceStore interface {
	// Search finds experiences relevant to a query.
	Search(ctx context.Context, query string, limit int) ([]Experience, error)
	// Save persists an experience.
	Save(ctx context.Context, exp Experience) error
}

// ExperienceInjector retrieves relevant past experiences and injects them as
// context for the current turn.
type ExperienceInjector struct {
	store ExperienceStore
}

// NewExperienceInjector creates an experience injector.
// Returns nil if store is nil (including typed nil).
func NewExperienceInjector(store ExperienceStore) *ExperienceInjector {
	if store == nil || isNil(store) {
		return nil
	}
	return &ExperienceInjector{store: store}
}

// GetRelevant retrieves experiences relevant to the user message.
func (ei *ExperienceInjector) GetRelevant(ctx context.Context, userMessage string, limit int) ([]Experience, error) {
	return ei.store.Search(ctx, userMessage, limit)
}

// FormatForPrompt renders experiences as an injection into the user message.
// Format: "[Prior learnings: ...] \n\n <user message>"
func FormatForPrompt(experiences []Experience, userMessage string) string {
	if len(experiences) == 0 {
		return userMessage
	}

	var b strings.Builder
	b.WriteString("[Prior learnings: ")
	for i, exp := range experiences {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(strings.TrimSpace(exp.Learning))
	}
	b.WriteString("]\n\n")
	b.WriteString(userMessage)
	return b.String()
}

// SaveExperience persists a learning from a completed turn.
func (ei *ExperienceInjector) SaveExperience(ctx context.Context, threadID, userMessage, response string) error {
	if ei.store == nil {
		return nil
	}

	// Extract a concise learning from the interaction.
	learning := extractLearning(userMessage, response)
	if learning == "" {
		return nil
	}

	return ei.store.Save(ctx, Experience{
		ThreadID: threadID,
		Message:  userMessage,
		Learning: learning,
		Context:  response,
	})
}

// extractLearning creates a concise summary of what was learned from an interaction.
// Uses heuristic cue-phrase matching as a fast path before any LLM-based reflection runs,
// mirroring the Rust extract_reflection_cues() approach.
func extractLearning(userMessage, response string) string {
	// Cue phrases indicating user preferences or explicit learnings.
	cuePhrases := []string{
		"i prefer", "i like", "i want you to", "from now on",
		"remember that i", "going forward", "always", "never",
		"i realized", "i discovered", "note to self",
	}

	var matches []string
	combined := strings.ToLower(userMessage + " " + response)
	for _, cue := range cuePhrases {
		idx := strings.Index(combined, cue)
		if idx < 0 {
			continue
		}
		end := idx + len(cue)
		if end > len(combined) {
			end = len(combined)
		}
		rest := combined[end:]
		// Take up to 120 chars after the cue as context.
		if len(rest) > 120 {
			rest = rest[:120]
		}
		sentenceEnd := strings.IndexAny(rest, ".!\n")
		if sentenceEnd > 0 {
			rest = rest[:sentenceEnd]
		}
		matches = append(matches, strings.TrimSpace(combined[idx:end]+rest))
	}

	if len(matches) == 0 {
		// Fallback: extract a concise topic from the user message.
		msg := userMessage
		if len(msg) > 120 {
			msg = msg[:120] + "..."
		}
		return "User asked: " + msg
	}

	if len(matches) > 3 {
		matches = matches[:3]
	}
	return strings.Join(matches, "; ")
}

// isNil reports whether v is nil, handling typed nil interfaces.
func isNil(v interface{}) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
		return rv.IsNil()
	}
	return false
}
