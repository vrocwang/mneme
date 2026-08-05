// Package memtools provides optional agent tools that depend on the memory
// pipeline. Kept in a separate package to avoid import cycles.
package memtools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/simon/mneme/internal/inference"
	"github.com/simon/mneme/internal/memory"
	"github.com/simon/mneme/internal/tools"
)

const (
	swMaxTurns       = 12
	swHardMaxTurns   = 25
	swMaxEvidence    = 30
	swTemp           = 0.2
	swMaxResultChars = 8000
)

// SmartWalkTool implements iterative LLM-driven memory retrieval matching
// Rust's smart_walk runner (runner.rs). Uses conversation history with
// inner tools dispatched each turn. Accumulates evidence with relevance
// and synthesizes a final answer with citations.
type SmartWalkTool struct {
	pipeline *memory.Pipeline
	provider inference.Provider
	model    string
}

func NewSmartWalkTool(pipeline *memory.Pipeline, provider inference.Provider, model string) tools.Tool {
	return &SmartWalkTool{pipeline: pipeline, provider: provider, model: model}
}

func (t *SmartWalkTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "smart_memory_walk",
		Description: `Deep iterative memory search with evidence collection. Walks through memory using multiple rounds of search, accumulating relevant evidence. Returns a synthesized answer with source citations. Use for complex research questions requiring multiple retrieval steps.`,
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"question": map[string]interface{}{
					"type":        "string",
					"description": "The research question to investigate.",
				},
				"max_turns": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum search turns (default 12, max 25).",
				},
			},
			"required": []string{"question"},
		},
	}
}

// ── Types matching Rust smart_walk/types.rs ──────────────────────────────

type swEvidence struct {
	Source    string `json:"source"`
	Snippet   string `json:"snippet"`
	Relevance string `json:"relevance"`
}

type swStep struct {
	Turn          int    `json:"turn"`
	Action        string `json:"action"`
	ArgsSummary   string `json:"args_summary"`
	ResultPreview string `json:"result_preview"`
}

type swStopReason string

const (
	swAnswered        swStopReason = "answered"
	swMaxTurnsReached swStopReason = "max_turns_reached"
	swError           swStopReason = "error"
	swLlmGaveUp       swStopReason = "llm_gave_up"
)

type swOutcome struct {
	Answer     string       `json:"answer"`
	Evidence   []swEvidence `json:"evidence"`
	Trace      []swStep     `json:"trace"`
	TurnsUsed  int          `json:"turns_used"`
	StopReason swStopReason `json:"stop_reason"`
}

// ── Inner tool dispatch table (matching Rust dispatch.rs) ────────────────

type innerTool func(ctx context.Context, t *SmartWalkTool, args map[string]interface{}, ev *[]swEvidence) (argsSummary, result string, isAnswer bool, answerText string)

var innerTools = map[string]innerTool{
	"keyword_search":   dispatchKeywordSearch,
	"vector_search":    dispatchVectorSearch,
	"collect_evidence": dispatchCollectEvidence,
	"answer":           dispatchAnswer,
}

func dispatchKeywordSearch(ctx context.Context, t *SmartWalkTool, args map[string]interface{}, _ *[]swEvidence) (string, string, bool, string) {
	pattern, _ := args["pattern"].(string)
	if pattern == "" {
		return "pattern=<empty>", "error: keyword_search requires a non-empty pattern", false, ""
	}
	result, err := t.pipeline.Search(ctx, pattern, 15)
	if err != nil {
		return fmt.Sprintf("pattern=%q", pattern), fmt.Sprintf("search error: %v", err), false, ""
	}
	if len(result.Scored) == 0 {
		return fmt.Sprintf("pattern=%q", pattern), fmt.Sprintf("no matches for %q", pattern), false, ""
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%d keyword matches:\n", len(result.Scored)))
	for _, sc := range result.Scored {
		preview := sc.Chunk.Content
		if len(preview) > 120 {
			preview = preview[:120] + "..."
		}
		b.WriteString(fmt.Sprintf("  [%d] (score=%.2f) %s\n", sc.Chunk.ID, sc.Score, preview))
	}
	return fmt.Sprintf("pattern=%q", pattern), b.String(), false, ""
}

func dispatchVectorSearch(ctx context.Context, t *SmartWalkTool, args map[string]interface{}, _ *[]swEvidence) (string, string, bool, string) {
	query, _ := args["query"].(string)
	if query == "" {
		return "query=<empty>", "error: vector_search requires a non-empty query", false, ""
	}
	result, err := t.pipeline.Search(ctx, query, 10)
	if err != nil {
		return fmt.Sprintf("query=%q", truncate(query, 40)), fmt.Sprintf("search error: %v", err), false, ""
	}
	if len(result.Scored) == 0 {
		return fmt.Sprintf("query=%q", truncate(query, 40)), fmt.Sprintf("no semantic matches for %q", query), false, ""
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%d semantic matches:\n", len(result.Scored)))
	for _, sc := range result.Scored {
		preview := sc.Chunk.Content
		if len(preview) > 120 {
			preview = preview[:120] + "..."
		}
		b.WriteString(fmt.Sprintf("  [%d] (score=%.2f) %s\n", sc.Chunk.ID, sc.Score, preview))
	}
	return fmt.Sprintf("query=%q", truncate(query, 40)), b.String(), false, ""
}

func dispatchCollectEvidence(_ context.Context, _ *SmartWalkTool, args map[string]interface{}, ev *[]swEvidence) (string, string, bool, string) {
	items, _ := args["items"].([]interface{})
	if len(items) == 0 {
		return "items=[]", "error: collect_evidence requires non-empty items array", false, ""
	}
	added := 0
	for _, item := range items {
		if len(*ev) >= swMaxEvidence {
			break
		}
		m, _ := item.(map[string]interface{})
		source, _ := m["source"].(string)
		snippet, _ := m["snippet"].(string)
		relevance, _ := m["relevance"].(string)
		if relevance == "" {
			relevance = "relevant"
		}
		if snippet != "" {
			*ev = append(*ev, swEvidence{Source: source, Snippet: snippet, Relevance: relevance})
			added++
		}
	}
	return fmt.Sprintf("%d items", added), fmt.Sprintf("collected %d evidence items (total: %d)", added, len(*ev)), false, ""
}

func dispatchAnswer(_ context.Context, _ *SmartWalkTool, args map[string]interface{}, _ *[]swEvidence) (string, string, bool, string) {
	text, _ := args["text"].(string)
	return "(final answer)", text, true, text
}

// ── Inner tool descriptions (matching Rust prompts.rs inner_tools) ────────

func innerToolsText() string {
	return `Available inner tools:

1. keyword_search(pattern: str) — Full-text search for the given pattern across all memory content. Returns matching files with preview snippets.

2. vector_search(query: str) — Semantic similarity search. Finds content conceptually related to the query even if keywords don't match exactly.

3. collect_evidence(items: [{source, snippet, relevance}]) — Save important findings as evidence for the final answer. Use after finding relevant content.

4. answer(text: str) — Provide the final synthesized answer. Include source citations inline. Call this when you have enough evidence to answer the question comprehensively.

Strategy: Start with keyword_search to find relevant content. Then use vector_search to find semantically related material. Use collect_evidence to save the most important findings. Finally call answer() with your synthesized response.`
}

func systemPrompt() string {
	return `You are a memory research agent. Your task is to investigate a question by searching the user's memory, collecting relevant evidence, and synthesizing a comprehensive answer with citations.

## Guidelines
- Be thorough: use multiple search strategies (keyword + vector) to find all relevant information.
- Be selective: only collect evidence that directly addresses the question.
- Be honest: if you cannot find sufficient evidence, say so in your answer.
- Cite sources: include source IDs in your final answer when referencing specific findings.
- Stop when done: call answer() as soon as you have enough evidence — don't search unnecessarily.
- Use at most 12 turns. If you haven't found enough by then, answer with what you have.`
}

// ── Main loop (matching Rust runner.rs run_smart_walk) ────────────────────

func (t *SmartWalkTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	question, _ := args["question"].(string)
	if question == "" {
		return tools.Result{Error: "question is required"}
	}
	if t.provider == nil {
		return tools.Result{Error: "no LLM provider configured for smart walk"}
	}

	maxTurns := swMaxTurns
	if mt, ok := args["max_turns"].(float64); ok && int(mt) > 0 && int(mt) <= swHardMaxTurns {
		maxTurns = int(mt)
	}

	// Build initial conversation.
	sysPrompt := systemPrompt() + "\n\n" + innerToolsText()
	inventory := t.buildInventory()

	history := []inference.Message{
		{Role: "system", Content: sysPrompt},
		{Role: "user", Content: fmt.Sprintf("Query: %s\n\n## Available memory\n%s", question, inventory)},
	}

	var trace []swStep
	var evidence []swEvidence

	for turn := 1; turn <= maxTurns; turn++ {
		response, err := t.chat(ctx, history)
		if err != nil {
			return tools.Result{Success: false, Output: formatOutcome(&swOutcome{
				Answer:     fmt.Sprintf("Walk failed on turn %d: %v. Partial results from %d turns.", turn, err, len(trace)),
				Evidence:   evidence,
				Trace:      trace,
				TurnsUsed:  turn,
				StopReason: swError,
			})}
		}

		if strings.TrimSpace(response) == "" {
			return tools.Result{Success: true, Output: formatOutcome(&swOutcome{
				Answer:     synthesizeFallback(trace, evidence),
				Evidence:   evidence,
				Trace:      trace,
				TurnsUsed:  turn,
				StopReason: swLlmGaveUp,
			})}
		}

		// Parse tool calls from response. Format: <tool_call>{"name":"...","args":{...}}</tool_call>
		calls := parseToolCalls(response)
		if len(calls) == 0 {
			return tools.Result{Success: true, Output: formatOutcome(&swOutcome{
				Answer:     strings.TrimSpace(response),
				Evidence:   evidence,
				Trace:      trace,
				TurnsUsed:  turn,
				StopReason: swAnswered,
			})}
		}

		history = append(history, inference.Message{Role: "assistant", Content: response})

		var combinedResults []string
		for _, call := range calls {
			fn, ok := innerTools[call.Name]
			if !ok {
				combinedResults = append(combinedResults, fmt.Sprintf("unknown action %q. Valid: keyword_search, vector_search, collect_evidence, answer", call.Name))
				continue
			}
			argsSummary, result, isAnswer, answerText := fn(ctx, t, call.Args, &evidence)
			preview := result
			if len(preview) > 200 {
				preview = preview[:200] + "..."
			}
			trace = append(trace, swStep{Turn: turn, Action: call.Name, ArgsSummary: argsSummary, ResultPreview: preview})

			if isAnswer {
				return tools.Result{Success: true, Output: formatOutcome(&swOutcome{
					Answer:     answerText,
					Evidence:   evidence,
					Trace:      trace,
					TurnsUsed:  turn,
					StopReason: swAnswered,
				})}
			}
			combinedResults = append(combinedResults, fmt.Sprintf("<tool_result name=%q>%s</tool_result>", call.Name, result))
		}

		// Append evidence summary to user message.
		evSummary := ""
		if len(evidence) > 0 {
			var es strings.Builder
			es.WriteString(fmt.Sprintf("\n\nEvidence collected so far (%d items):\n", len(evidence)))
			for i, e := range evidence {
				snippet := e.Snippet
				if len(snippet) > 200 {
					snippet = snippet[:200] + "..."
				}
				es.WriteString(fmt.Sprintf("  %d. [%s] %s (relevance: %s)\n", i+1, e.Source, snippet, e.Relevance))
			}
			evSummary = es.String()
		}
		history = append(history, inference.Message{Role: "user", Content: strings.Join(combinedResults, "\n\n") + evSummary})
	}

	return tools.Result{Success: true, Output: formatOutcome(&swOutcome{
		Answer:     synthesizeFallback(trace, evidence),
		Evidence:   evidence,
		Trace:      trace,
		TurnsUsed:  maxTurns,
		StopReason: swMaxTurnsReached,
	})}
}

func (t *SmartWalkTool) chat(ctx context.Context, messages []inference.Message) (string, error) {
	tokens, errs := t.provider.Chat(ctx, inference.ChatRequest{
		Model:       t.model,
		Messages:    messages,
		MaxTokens:   1024,
		Temperature: swTemp,
	})
	var response string
	for tok := range tokens {
		response += tok.Text
	}
	var err error
	for e := range errs {
		if e != nil {
			err = e
		}
	}
	return response, err
}

func (t *SmartWalkTool) buildInventory() string {
	if t.pipeline == nil {
		return "(memory pipeline unavailable — use keyword_search or vector_search to explore)"
	}
	result, err := t.pipeline.Search(context.Background(), "recent", 5)
	if err != nil || result == nil || len(result.Scored) == 0 {
		return "(memory pipeline available — use keyword_search or vector_search to explore)"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%d recent memory entries available. Top entries:\n", len(result.Scored)))
	for _, sc := range result.Scored {
		preview := sc.Chunk.Content
		if len(preview) > 100 {
			preview = preview[:100] + "..."
		}
		b.WriteString(fmt.Sprintf("  [%d] %s\n", sc.Chunk.ID, preview))
	}
	return b.String()
}

// ── Tool call parsing ─────────────────────────────────────────────────────

type toolCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

func parseToolCalls(response string) []toolCall {
	var calls []toolCall
	// Parse <tool_call>{"name":"...","args":{...}}</tool_call>
	for {
		start := strings.Index(response, "<tool_call>")
		if start < 0 {
			break
		}
		start += 11
		end := strings.Index(response[start:], "</tool_call>")
		if end < 0 {
			break
		}
		jsonStr := response[start : start+end]
		var call toolCall
		if err := json.Unmarshal([]byte(jsonStr), &call); err == nil && call.Name != "" {
			calls = append(calls, call)
		}
		response = response[start+end+12:]
	}
	return calls
}

// ── Output formatting ─────────────────────────────────────────────────────

func formatOutcome(o *swOutcome) string {
	var b strings.Builder
	b.WriteString(o.Answer)
	b.WriteString(fmt.Sprintf("\n\n---\n*Walk completed in %d turns. Stop reason: %s.*\n", o.TurnsUsed, o.StopReason))
	if len(o.Evidence) > 0 {
		b.WriteString(fmt.Sprintf("\n**Evidence (%d items):**\n", len(o.Evidence)))
		for i, e := range o.Evidence {
			snippet := e.Snippet
			if len(snippet) > 150 {
				snippet = snippet[:150] + "..."
			}
			b.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, e.Source, snippet))
		}
	}
	return b.String()
}

func synthesizeFallback(trace []swStep, evidence []swEvidence) string {
	if len(evidence) == 0 {
		return "No evidence collected. Try a more specific query or broaden your search terms."
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## Partial Results\n\nBased on %d evidence items collected over %d turns:\n\n", len(evidence), len(trace)))
	for i, e := range evidence {
		snippet := e.Snippet
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		b.WriteString(fmt.Sprintf("**%d.** [%s] %s\n\n", i+1, e.Source, snippet))
	}
	b.WriteString("*(Answer incomplete — max turns reached before final synthesis)*")
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func (t *SmartWalkTool) ConcurrencySafe() bool                  { return true }
func (t *SmartWalkTool) PermissionLevel() tools.PermissionLevel { return tools.PermReadOnly }
