package context

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/simon/mneme/internal/inference"
)

// Segment represents a timestamped chunk of conversation.
type Segment struct {
	Timestamp time.Time `json:"timestamp"`
	Text      string    `json:"text"`
	Speaker   string    `json:"speaker"`
}

// SegmentRecapSummarizer produces consolidated recaps from conversation segments.
type SegmentRecapSummarizer struct {
	provider inference.Provider
}

// NewSegmentRecapSummarizer creates a segment recap summarizer.
func NewSegmentRecapSummarizer(provider inference.Provider) *SegmentRecapSummarizer {
	return &SegmentRecapSummarizer{provider: provider}
}

// Recap produces a condensed recap from a list of conversation segments.
// Falls back to heuristic truncation if no provider is available.
func (s *SegmentRecapSummarizer) Recap(ctx context.Context, segments []Segment, model string) (string, error) {
	if len(segments) == 0 {
		return "", nil
	}

	if s.provider == nil {
		return heuristicRecap(segments), nil
	}

	prompt := buildRecapPrompt(segments)

	req := inference.ChatRequest{
		Model:    model,
		Messages: []inference.Message{{Role: "user", Content: prompt}},
		SystemPrompt: `You are a precise conversation summarizer. Produce a chronological recap under 800 characters.
Include: key decisions, action items, and context shifts. Be concise. Output ONLY the recap text.`,
		MaxTokens:   300,
		Temperature: 0.2,
	}

	tokens, errs := s.provider.Chat(ctx, req)
	var result strings.Builder
	for {
		select {
		case tok, ok := <-tokens:
			if !ok {
				goto done
			}
			result.WriteString(tok.Text)
		case err, ok := <-errs:
			if ok && err != nil {
				return heuristicRecap(segments), fmt.Errorf("recap LLM: %w", err)
			}
		case <-ctx.Done():
			return heuristicRecap(segments), ctx.Err()
		}
	}
done:
	out := strings.TrimSpace(result.String())
	if out == "" {
		return heuristicRecap(segments), nil
	}
	return out, nil
}

func buildRecapPrompt(segments []Segment) string {
	var sb strings.Builder
	sb.WriteString("Summarize the following conversation segments into a chronological recap:\n\n")
	for _, seg := range segments {
		ts := seg.Timestamp.Format("15:04")
		speaker := seg.Speaker
		if speaker == "" {
			speaker = "Unknown"
		}
		fmt.Fprintf(&sb, "[%s] %s: %s\n", ts, speaker, seg.Text)
	}
	return sb.String()
}

func heuristicRecap(segments []Segment) string {
	var sb strings.Builder
	for _, seg := range segments {
		text := seg.Text
		if len(text) > 120 {
			text = text[:120] + "..."
		}
		sb.WriteString(text)
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}
