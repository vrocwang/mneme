package subconscious

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/simon/mneme/internal/inference"
)

// LLMEvaluatorConfig tunes the LLM-based background evaluator.
type LLMEvaluatorConfig struct {
	RunEveryN    int // ticks between LLM evaluations (default 6 = every 3 hours)
	MaxSummaries int // max tree summaries in report
	MaxTokens    int // max LLM response tokens
}

func DefaultLLMEvaluatorConfig() LLMEvaluatorConfig {
	return LLMEvaluatorConfig{RunEveryN: 6, MaxSummaries: 8, MaxTokens: 16384}
}

// LLMEvaluator periodically analyzes memory, preferences, and scratchpad
// via an LLM to produce richer proactive actions than heuristic evaluators.
// Runs every Nth tick; heuristic evaluators provide zero-cost coverage on
// every tick.
//
// Scratchpad provides cross-tick persistent working memory — the LLM can
// add/remove entries that persist across evaluations. Environment context
// (hostname, OS, workspace) and pre-LLM conversation retrieval give the
// LLM the same grounding that Rust's engine.rs tick_inner() provides.
type LLMEvaluator struct {
	log      *slog.Logger
	provider inference.Provider
	model    string
	config   LLMEvaluatorConfig

	pipeline   MemoryPipeline
	learning   PrefsFunc
	workspace  string
	scratchpad *Scratchpad

	tickCount        int
	consecutiveFails int
	backoffUntil     time.Time
}

// PrefsFunc returns learned user preferences for the LLM evaluator.
type PrefsFunc func() []PreferencePair

// PreferencePair is a key-value preference with confidence.
type PreferencePair struct {
	Key        string
	Value      string
	Confidence float64
}

func NewLLMEvaluator(log *slog.Logger, provider inference.Provider, model string, pipeline MemoryPipeline) *LLMEvaluator {
	if log == nil {
		log = slog.Default()
	}
	return &LLMEvaluator{
		log:      log,
		provider: provider,
		model:    model,
		pipeline: pipeline,
		config:   DefaultLLMEvaluatorConfig(),
	}
}

func (e *LLMEvaluator) WithConfig(cfg LLMEvaluatorConfig) *LLMEvaluator {
	e.config = cfg
	return e
}

func (e *LLMEvaluator) WithPrefs(fn PrefsFunc) *LLMEvaluator {
	e.learning = fn
	return e
}

// WithWorkspace sets the workspace directory for environment context and scratchpad.
func (e *LLMEvaluator) WithWorkspace(dir string) *LLMEvaluator {
	e.workspace = dir
	e.scratchpad = NewScratchpad(dir)
	return e
}

func (e *LLMEvaluator) Name() string { return "llm_evaluator" }

func (e *LLMEvaluator) Evaluate(ctx context.Context, state *EngineState) ([]Action, error) {
	e.tickCount++
	if e.tickCount%e.config.RunEveryN != 0 {
		return nil, nil
	}
	if e.provider == nil {
		return nil, nil
	}

	// Backoff: skip evaluation if within backoff window after repeated failures.
	if time.Now().Before(e.backoffUntil) {
		return nil, nil
	}

	// Load scratchpad for cross-tick continuity.
	if e.scratchpad != nil {
		_ = e.scratchpad.Load() // non-fatal
	}

	report := e.buildSituationReport(state)
	e.log.Debug("llm evaluator: situation report built", "length", len(report))

	actions, err := e.queryLLM(ctx, report)
	if err != nil {
		e.consecutiveFails++
		e.log.Warn("llm evaluator: LLM query failed", "error", err, "consecutiveFails", e.consecutiveFails)
		// Exponential backoff: 2^n * RunEveryN ticks, capped at ~24h worth.
		if e.consecutiveFails >= 3 {
			backoffTicks := 1 << min(e.consecutiveFails-3, 5) // max 32x multiplier
			e.backoffUntil = time.Now().Add(time.Duration(backoffTicks*e.config.RunEveryN) * 30 * time.Second)
		}
		return nil, nil
	}

	e.consecutiveFails = 0

	// Merge scratchpad directives from LLM output and persist.
	if e.scratchpad != nil {
		actions = e.scratchpad.MergeActions(actions)
	}

	e.log.Info("llm evaluator: produced actions", "count", len(actions), "scratchpad_entries", func() int {
		if e.scratchpad != nil {
			return e.scratchpad.Len()
		}
		return 0
	}())
	return actions, nil
}

const maxReportChars = 4000

func (e *LLMEvaluator) buildSituationReport(state *EngineState) string {
	var b strings.Builder
	b.WriteString("You are a background awareness agent. Review the following context and identify patterns, risks, or opportunities.\n\n")

	// ── Environment section (matches Rust's build_environment_section) ──
	e.writeEnvironment(&b)

	// ── Memory summaries since last tick ──
	if e.pipeline != nil && b.Len() < maxReportChars/2 {
		query := "sealed since " + state.LastTickAt.Format("2006-01-02")
		result, err := e.pipeline.Search(query, e.config.MaxSummaries)
		if err == nil && result != nil && result.TotalCount > 0 {
			b.WriteString("## Recent Memory Activity (since last tick)\n\n")
			for _, item := range result.Items {
				if b.Len() > maxReportChars/2 {
					break
				}
				b.WriteString("- ")
				b.WriteString(item)
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
	}

	// ── Pre-LLM conversation memory retrieval ──
	if e.pipeline != nil && b.Len() < maxReportChars*2/3 {
		convResult, err := e.pipeline.Search("recent conversation summary decisions", 3)
		if err == nil && convResult != nil && convResult.TotalCount > 0 {
			b.WriteString("## Recent Conversation Context\n\n")
			for _, item := range convResult.Items {
				if b.Len() > maxReportChars*2/3 {
					break
				}
				b.WriteString("- ")
				b.WriteString(item)
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
	}

	// ── Learned preferences ──
	if e.learning != nil && b.Len() < maxReportChars*3/4 {
		prefs := e.learning()
		if len(prefs) > 0 {
			b.WriteString("## Learned User Preferences\n\n")
			for _, p := range prefs {
				if b.Len() > maxReportChars*3/4 {
					break
				}
				b.WriteString(fmt.Sprintf("- %s: %s (confidence: %.2f)\n", p.Key, p.Value, p.Confidence))
			}
			b.WriteString("\n")
		}
	}

	// ── Scratchpad (persistent working memory) ──
	if e.scratchpad != nil && e.scratchpad.Len() > 0 && b.Len() < maxReportChars {
		remaining := maxReportChars - b.Len()
		if remaining > 500 {
			section := e.scratchpad.RenderForPrompt(remaining / 4)
			b.WriteString(section)
		}
	}

	// ── Recent reflections ──
	if len(state.RecentReflections) > 0 && b.Len() < maxReportChars {
		b.WriteString("## Recent Reflections\n\n")
		for _, r := range state.RecentReflections {
			if b.Len() > maxReportChars {
				break
			}
			b.WriteString(fmt.Sprintf("- [%s] %s\n", r.Kind, truncateStr(r.Body, 200)))
		}
		b.WriteString("\n")
	}

	b.WriteString("Produce a JSON array of observations. Each observation: {\"type\":\"suggestion|task|reflection|scratchpad_add|scratchpad_remove\",\"title\":\"...\",\"body\":\"...\",\"priority\":1-10}. ")
	b.WriteString("Use scratchpad_add to persist important findings across ticks (body and priority fields). ")
	b.WriteString("Use scratchpad_remove to clean up stale entries (id field). ")
	b.WriteString("Focus on patterns, risks, and opportunities. Be concise. Return ONLY the JSON array.")

	return b.String()
}

func (e *LLMEvaluator) writeEnvironment(b *strings.Builder) {
	hostname, _ := os.Hostname()
	b.WriteString("## Environment\n\n")
	b.WriteString(fmt.Sprintf("- Workspace: %s\n", e.workspace))
	b.WriteString(fmt.Sprintf("- Hostname: %s\n", hostname))
	b.WriteString(fmt.Sprintf("- OS: %s\n", runtime.GOOS))
	b.WriteString(fmt.Sprintf("- Time: %s\n\n", timeStr()))

	// Load SOUL.md for identity context (matches Rust's load_identity_context).
	if e.workspace != "" {
		soulPath := filepath.Join(e.workspace, "SOUL.md")
		if data, err := os.ReadFile(soulPath); err == nil && len(data) > 0 {
			b.WriteString("## Identity (SOUL.md)\n\n")
			b.WriteString(string(data))
			b.WriteString("\n\n")
		}
	}
}

func timeStr() string { return time.Now().Format(time.RFC3339) }

type llmAction struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Priority int    `json:"priority"`
}

func (e *LLMEvaluator) queryLLM(ctx context.Context, report string) ([]Action, error) {
	tokens, errs := e.provider.Chat(ctx, inference.ChatRequest{
		Model: e.model,
		Messages: []inference.Message{
			{Role: "system", Content: "You are a JSON output agent. Respond with ONLY a raw JSON array (no markdown fences, no explanation). Maximum 3 actions. Keep each action body under 200 characters. Example: [{\"type\":\"suggestion\",\"title\":\"...\",\"body\":\"...\",\"priority\":5}]"},
			{Role: "user", Content: report},
		},
		MaxTokens:   e.config.MaxTokens,
		Temperature: 0.3,
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
	if err != nil {
		return nil, fmt.Errorf("llm call: %w", err)
	}

	return e.parseActions(response)
}

func (e *LLMEvaluator) parseActions(response string) ([]Action, error) {
	// Strip markdown code fences if present.
	cleaned := response
	if idx := strings.Index(cleaned, "```"); idx >= 0 {
		cleaned = cleaned[idx+3:]
		if i := strings.Index(cleaned, "\n"); i >= 0 {
			cleaned = cleaned[i+1:]
		}
		if idx := strings.LastIndex(cleaned, "```"); idx >= 0 {
			cleaned = cleaned[:idx]
		}
	}

	// Find JSON array brackets.
	start := strings.Index(cleaned, "[")
	end := strings.LastIndex(cleaned, "]")
	if start >= 0 && end > start {
		slice := cleaned[start : end+1]
		var llmActions []llmAction
		if err := json.Unmarshal([]byte(slice), &llmActions); err == nil {
			return e.buildActions(llmActions)
		}
	}

	// Fallback: response may be truncated. Extract individual complete JSON
	// objects by scanning for balanced braces. This handles truncated arrays
	// (missing ]), markdown fences, and partial responses robustly.
	objects := extractJSONObjects(cleaned)
	if len(objects) > 0 {
		var llmActions []llmAction
		for _, obj := range objects {
			var a llmAction
			if json.Unmarshal([]byte(obj), &a) == nil && a.Type != "" {
				llmActions = append(llmActions, a)
			}
		}
		if len(llmActions) > 0 {
			e.log.Debug("llm evaluator: extracted objects from response", "actions", len(llmActions))
			return e.buildActions(llmActions)
		}
	}

	if len(response) > 0 {
		e.log.Warn("llm evaluator: failed to parse JSON", "response", response[:min(len(response), 500)])
	} else {
		e.log.Debug("llm evaluator: empty response from provider")
	}
	return nil, fmt.Errorf("no JSON array in response")
}

func (e *LLMEvaluator) buildActions(llmActions []llmAction) ([]Action, error) {

	actions := make([]Action, 0, len(llmActions))
	for _, la := range llmActions {
		if la.Type == "scratchpad_add" || la.Type == "scratchpad_remove" {
			actions = append(actions, Action{
				Type: la.Type,
				Payload: map[string]interface{}{
					"body": la.Body, "title": la.Title,
					"priority": float64(la.Priority), "id": la.Title,
				},
			})
			continue
		}
		if la.Title == "" || la.Body == "" {
			continue
		}
		actions = append(actions, Action{
			Type:    la.Type,
			Title:   la.Title,
			Message: la.Body,
			Payload: map[string]interface{}{"priority": la.Priority},
		})
	}
	return actions, nil
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// extractJSONObjects scans a string for balanced JSON objects ({...}).
// Correctly handles braces inside string values and escape sequences.
// Returns only complete objects - truncated objects (unbalanced braces) are skipped.
// This is the core fix for truncated LLM responses: even if the JSON array
// is incomplete (missing ]), each complete object within it is extracted.
func extractJSONObjects(s string) []string {
	var objects []string
	depth := 0
	start := -1
	inString := false
	escaped := false

	for i, r := range s {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && inString {
			escaped = true
			continue
		}
		if r == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if r == '{' {
			if depth == 0 {
				start = i
			}
			depth++
		}
		if r == '}' {
			depth--
			if depth == 0 && start >= 0 {
				objects = append(objects, s[start:i+1])
				start = -1
			}
		}
	}
	return objects
}
