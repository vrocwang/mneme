package agent

import (
	"context"
	"strings"
)

// TranscriptIngestor processes session transcripts to extract durable memories.
type TranscriptIngestor struct {
	pipeline MemoryPipelineIngestor
}

// MemoryPipelineIngestor is the interface for feeding extracted content into the memory system.
type MemoryPipelineIngestor interface {
	IndexContent(source, content string) error
	ArchiveConversation(threadID string) error
}

// NewTranscriptIngestor creates an ingestor that feeds into the memory pipeline.
func NewTranscriptIngestor(pipeline MemoryPipelineIngestor) *TranscriptIngestor {
	return &TranscriptIngestor{pipeline: pipeline}
}

// Ingest processes a completed turn's transcript and extracts memories.
// It runs asynchronously and should be called via go-routine.
func (ti *TranscriptIngestor) Ingest(ctx context.Context, workspaceDir, threadID, userMessage, assistantResponse string) {
	if ti.pipeline == nil {
		return
	}

	// Index the conversation as a memory source.
	content := buildConversationContent(threadID, userMessage, assistantResponse)
	if content != "" {
		_ = ti.pipeline.IndexContent("conversation:"+threadID, content)
	}

	// Extract key facts heuristically.
	facts := extractKeyFacts(userMessage, assistantResponse)
	for _, fact := range facts {
		if fact != "" {
			_ = ti.pipeline.IndexContent("fact:"+threadID, fact)
		}
	}
}

// buildConversationContent creates a structured record of the conversation turn.
func buildConversationContent(threadID, userMessage, assistantResponse string) string {
	var b strings.Builder
	b.WriteString("User: ")
	b.WriteString(userMessage)
	b.WriteString("\n\nAssistant: ")
	if len(assistantResponse) > 2000 {
		assistantResponse = assistantResponse[:2000] + "..."
	}
	b.WriteString(assistantResponse)
	return b.String()
}

// extractKeyFacts extracts named entities and key statements from a conversation.
func extractKeyFacts(userMessage, assistantResponse string) []string {
	var facts []string

	// Extract topic from user message.
	topic := extractTopic(userMessage)
	if topic != "" {
		facts = append(facts, "Topic: "+topic)
	}

	// Extract entities mentioned in the conversation.
	entities := extractEntities(userMessage + " " + truncateStr(assistantResponse, 1000))
	for _, e := range entities {
		if e != "" {
			facts = append(facts, "Entity: "+e)
		}
	}

	// Extract decisions when the assistant explicitly states a conclusion.
	if strings.Contains(assistantResponse, "decision:") ||
		strings.Contains(assistantResponse, "conclusion:") ||
		strings.Contains(assistantResponse, "decided to") {
		facts = append(facts, "Decision made in conversation: "+truncateStr(assistantResponse, 300))
	}

	return facts
}

// extractTopic extracts the main topic from a user message.
func extractTopic(message string) string {
	// Remove common prefixes.
	cleaned := strings.TrimSpace(message)
	prefixes := []string{"can you ", "please ", "help me ", "i need ", "i want ", "how do i ", "what is ", "tell me about "}
	for _, p := range prefixes {
		if strings.HasPrefix(strings.ToLower(cleaned), p) {
			cleaned = cleaned[len(p):]
			break
		}
	}
	if len(cleaned) > 100 {
		cleaned = cleaned[:100] + "..."
	}
	return cleaned
}

// extractEntities finds named entities in text using simple heuristics.
func extractEntities(text string) []string {
	var entities []string
	seen := make(map[string]bool)

	words := strings.Fields(text)
	for i, word := range words {
		// Capitalized words that aren't sentence starters or common words.
		if len(word) > 1 && word[0] >= 'A' && word[0] <= 'Z' {
			// Skip sentence starters (first word of text or after period).
			if i == 0 || (i > 0 && strings.HasSuffix(words[i-1], ".")) {
				continue
			}
			// Skip common capitalized words.
			commonWords := map[string]bool{
				"I": true, "I'm": true, "I'll": true, "I've": true, "It": true, "It's": true,
				"The": true, "This": true, "That": true, "These": true, "Those": true,
				"A": true, "An": true, "And": true, "But": true, "Or": true, "So": true,
				"In": true, "On": true, "At": true, "To": true, "For": true, "With": true,
				"From": true, "By": true, "As": true, "If": true, "We": true, "You": true,
				"Not": true, "No": true, "Yes": true,
			}
			if commonWords[word] {
				continue
			}
			if !seen[word] {
				entities = append(entities, word)
				seen[word] = true
			}
		}

		// Detect potential multi-word proper nouns (e.g., "Docker Compose", "GitHub Actions").
		if i+1 < len(words) && len(word) > 1 && word[0] >= 'A' && word[0] <= 'Z' {
			next := words[i+1]
			if len(next) > 1 && next[0] >= 'A' && next[0] <= 'Z' {
				combined := word + " " + next
				if !seen[combined] {
					entities = append(entities, combined)
					seen[combined] = true
				}
			}
		}
	}

	if len(entities) > 10 {
		entities = entities[:10]
	}
	return entities
}
