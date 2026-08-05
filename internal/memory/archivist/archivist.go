package archivist

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/simon/mneme/internal/inference"
	"github.com/simon/mneme/internal/memory/entities"
)

// Archivist curates long-term memory using an LLM provider.
// It summarizes, deduplicates, and extracts key facts from memory chunks.
type Archivist struct {
	log      *slog.Logger
	provider inference.Provider
	model    string
}

// New creates a new Archivist.
func New(log *slog.Logger, provider inference.Provider, model string) *Archivist {
	return &Archivist{log: log, provider: provider, model: model}
}

// SummaryResult is the output of a summarization pass.
type SummaryResult struct {
	Summary     string   `json:"summary"`
	KeyFacts    []string `json:"key_facts"`
	Entities    []string `json:"entities"`
	ShouldPrune bool     `json:"should_prune"`
}

// SummarizeMemory produces a concise summary of a memory chunk.
// This is used for the "seal" step in the memory pipeline — converting raw
// conversation content into curated long-term memory.
func (a *Archivist) SummarizeMemory(ctx context.Context, content string) (*SummaryResult, error) {
	if a.provider == nil {
		return heuristicSummary(content), nil
	}

	prompt := fmt.Sprintf(`You are a memory curator. Summarize the following content for long-term memory storage.

Rules:
- Write a 2-4 sentence summary capturing the essential information
- Extract 1-5 key facts as bullet points
- Identify named entities (people, organizations, projects, technologies)
- Mark should_prune: true ONLY if the content is trivial/empty (greetings, acknowledgments without substance)

Content:
%s

Respond in JSON format:
{"summary": "...", "key_facts": ["..."], "entities": ["..."], "should_prune": false}`, truncateForPrompt(content, 4000))

	req := inference.ChatRequest{
		Model: a.model,
		Messages: []inference.Message{
			{Role: "user", Content: prompt},
		},
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	tokens, errs := a.provider.Chat(ctx, req)

	var response string
loop:
	for {
		select {
		case tok, ok := <-tokens:
			if !ok {
				break loop
			}
			response += tok.Text
		case err, ok := <-errs:
			if ok && err != nil {
				a.log.Warn("archivist LLM error, using heuristic", "error", err)
				return heuristicSummary(content), nil
			}
		case <-ctx.Done():
			return heuristicSummary(content), nil
		}
	}

	result, err := parseSummaryResponse(response)
	if err != nil {
		a.log.Debug("archivist parse error, using heuristic", "error", err)
		return heuristicSummary(content), nil
	}
	return result, nil
}

// Deduplicate checks whether a new memory chunk is a near-duplicate of an existing one.
// Returns a similarity score (0-1) and an optional merged summary.
func (a *Archivist) Deduplicate(ctx context.Context, existing, newContent string) (float64, string, error) {
	if a.provider == nil {
		return simpleSimilarity(existing, newContent), "", nil
	}

	prompt := fmt.Sprintf(`Compare two memory entries and determine if they are duplicates.

Entry A:
%s

Entry B:
%s

Respond in JSON:
{"score": 0.0, "merged": ""}

score: 0.0 = completely different topics, 0.5 = related, 0.9+ = near-duplicate
merged: if score >= 0.8, provide a merged summary combining both entries. Otherwise leave empty.`,
		truncateForPrompt(existing, 2000), truncateForPrompt(newContent, 2000))

	req := inference.ChatRequest{
		Model: a.model,
		Messages: []inference.Message{
			{Role: "user", Content: prompt},
		},
	}

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	tokens, errs := a.provider.Chat(ctx, req)

	var response string
loop:
	for {
		select {
		case tok, ok := <-tokens:
			if !ok {
				break loop
			}
			response += tok.Text
		case err, ok := <-errs:
			if ok && err != nil {
				return simpleSimilarity(existing, newContent), "", nil
			}
		case <-ctx.Done():
			return simpleSimilarity(existing, newContent), "", nil
		}
	}

	var result struct {
		Score  float64 `json:"score"`
		Merged string  `json:"merged"`
	}
	// Simple JSON extraction from LLM response
	if err := extractJSON(response, &result); err == nil {
		return result.Score, result.Merged, nil
	}
	return simpleSimilarity(existing, newContent), "", nil
}

// ExtractFacts pulls key facts from content without full summarization.
func (a *Archivist) ExtractFacts(ctx context.Context, content string) ([]string, error) {
	if a.provider == nil {
		return heuristicFacts(content), nil
	}

	prompt := fmt.Sprintf(`Extract 3-8 key facts from the following content. Each fact should be a single, self-contained sentence. Only include substantive facts — skip pleasantries and meta-commentary.

Content:
%s

Respond as JSON:
{"facts": ["fact 1", "fact 2", ...]}`, truncateForPrompt(content, 4000))

	req := inference.ChatRequest{
		Model: a.model,
		Messages: []inference.Message{
			{Role: "user", Content: prompt},
		},
	}

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	tokens, errs := a.provider.Chat(ctx, req)

	var response string
loop:
	for {
		select {
		case tok, ok := <-tokens:
			if !ok {
				break loop
			}
			response += tok.Text
		case err, ok := <-errs:
			if ok && err != nil {
				return heuristicFacts(content), nil
			}
		case <-ctx.Done():
			return heuristicFacts(content), nil
		}
	}

	var result struct {
		Facts []string `json:"facts"`
	}
	if err := extractJSON(response, &result); err == nil && len(result.Facts) > 0 {
		return result.Facts, nil
	}
	return heuristicFacts(content), nil
}

// ── Heuristic fallbacks (no provider) ─────────────────────────────

func heuristicSummary(content string) *SummaryResult {
	sentences := splitSentences(content)
	summary := content
	if len(content) > 500 {
		// Take first 2 and last 1 sentences as a heuristic summary
		if len(sentences) >= 3 {
			summary = sentences[0] + " " + sentences[1] + " ... " + sentences[len(sentences)-1]
		} else {
			summary = content[:500]
		}
	}

	shouldPrune := len(strings.TrimSpace(content)) < 50

	return &SummaryResult{
		Summary:     summary,
		KeyFacts:    heuristicFacts(content),
		Entities:    extractEntitiesHeuristic(content),
		ShouldPrune: shouldPrune,
	}
}

func heuristicFacts(content string) []string {
	sentences := splitSentences(content)
	var facts []string
	for _, s := range sentences {
		s = strings.TrimSpace(s)
		if len(s) > 30 && len(s) < 300 {
			facts = append(facts, s)
		}
	}
	if len(facts) > 5 {
		facts = facts[:5]
	}
	return facts
}

func extractEntitiesHeuristic(content string) []string {
	canonical := entities.ExtractFromText(content)
	result := make([]string, 0, len(canonical))
	for _, e := range canonical {
		result = append(result, e.Name)
	}
	if len(result) > 10 {
		result = result[:10]
	}
	return result
}

func simpleSimilarity(a, b string) float64 {
	wordsA := strings.Fields(strings.ToLower(a))
	wordsB := strings.Fields(strings.ToLower(b))
	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0
	}
	setA := make(map[string]bool, len(wordsA))
	for _, w := range wordsA {
		setA[w] = true
	}
	common := 0
	seen := make(map[string]bool)
	for _, w := range wordsB {
		if setA[w] && !seen[w] {
			common++
			seen[w] = true
		}
	}
	// Jaccard-like: size of intersection divided by size of union.
	// wordsB may contain duplicates; deduplicate it for the denominator.
	setBSize := len(setA) // approximate: use setA size as a proxy for setB uniqueness
	for _, w := range wordsB {
		if !setA[w] {
			setBSize++
		}
	}
	if setBSize == 0 {
		return 0
	}
	return float64(common) / float64(setBSize)
}

// ── Helpers ─────────────────────────────────────────────────────

func splitSentences(text string) []string {
	var sentences []string
	current := strings.Builder{}
	for _, r := range text {
		current.WriteRune(r)
		if r == '.' || r == '!' || r == '?' || r == '\n' {
			s := strings.TrimSpace(current.String())
			if len(s) > 0 {
				sentences = append(sentences, s)
			}
			current.Reset()
		}
	}
	if current.Len() > 0 {
		s := strings.TrimSpace(current.String())
		if len(s) > 0 {
			sentences = append(sentences, s)
		}
	}
	return sentences
}

func truncateForPrompt(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n...[truncated]"
}

func parseSummaryResponse(response string) (*SummaryResult, error) {
	var r SummaryResult
	if err := extractJSON(response, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func extractJSON(s string, target interface{}) error {
	// Find JSON object in the response text
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start == -1 || end == -1 || end <= start {
		return fmt.Errorf("no JSON object found")
	}
	jsonStr := s[start : end+1]
	// Very simple manual parse for the known keys (avoids encoding/json dependency complexity)
	return simpleJSONParse(jsonStr, target)
}

func simpleJSONParse(jsonStr string, target interface{}) error {
	// Lightweight parser that handles our specific response formats.
	// Falls back to string-based extraction for the known structs.
	switch t := target.(type) {
	case *SummaryResult:
		t.Summary = extractStringField(jsonStr, "summary")
		t.KeyFacts = extractStringArray(jsonStr, "key_facts")
		t.Entities = extractStringArray(jsonStr, "entities")
		t.ShouldPrune = strings.Contains(jsonStr, `"should_prune": true`) ||
			strings.Contains(jsonStr, `"should_prune":true`)
		return nil
	case *struct {
		Score  float64 `json:"score"`
		Merged string  `json:"merged"`
	}:
		t.Score = extractFloatField(jsonStr, "score")
		t.Merged = extractStringField(jsonStr, "merged")
		return nil
	case *struct {
		Facts []string `json:"facts"`
	}:
		t.Facts = extractStringArray(jsonStr, "facts")
		return nil
	}
	return fmt.Errorf("unsupported target type")
}

func extractStringField(json, key string) string {
	// Find "key": "value" or "key":"value"
	search := fmt.Sprintf(`"%s"`, key)
	idx := strings.Index(json, search)
	if idx == -1 {
		return ""
	}
	rest := json[idx+len(search):]
	colon := strings.Index(rest, ":")
	if colon == -1 {
		return ""
	}
	rest = rest[colon+1:]
	rest = strings.TrimLeft(rest, " \t\n\r")

	if strings.HasPrefix(rest, `"`) {
		rest = rest[1:]
		end := strings.Index(rest, `"`)
		if end == -1 {
			return rest
		}
		return unescapeJSON(rest[:end])
	}
	return ""
}

func extractStringArray(json, key string) []string {
	search := fmt.Sprintf(`"%s"`, key)
	idx := strings.Index(json, search)
	if idx == -1 {
		return nil
	}
	rest := json[idx+len(search):]
	colon := strings.Index(rest, ":")
	if colon == -1 {
		return nil
	}
	rest = rest[colon+1:]
	rest = strings.TrimLeft(rest, " \t\n\r")

	if !strings.HasPrefix(rest, "[") {
		return nil
	}
	rest = rest[1:] // skip [
	closeBracket := strings.Index(rest, "]")
	if closeBracket == -1 {
		return nil
	}
	rest = rest[:closeBracket]

	var items []string
	for _, part := range strings.Split(rest, ",") {
		part = strings.TrimSpace(part)
		part = strings.Trim(part, `"`)
		if part != "" {
			items = append(items, unescapeJSON(part))
		}
	}
	return items
}

func extractFloatField(json, key string) float64 {
	search := fmt.Sprintf(`"%s"`, key)
	idx := strings.Index(json, search)
	if idx == -1 {
		return 0
	}
	rest := json[idx+len(search):]
	colon := strings.Index(rest, ":")
	if colon == -1 {
		return 0
	}
	rest = strings.TrimLeft(rest[colon+1:], " \t\n\r")
	var value float64
	fmt.Sscanf(rest, "%f", &value)
	return value
}

func unescapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\\`, `\`)
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\t`, "\t")
	return s
}
