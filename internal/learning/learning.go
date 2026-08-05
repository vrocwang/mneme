package learning

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/simon/mneme/internal/agent"
	"github.com/simon/mneme/internal/inference"
	"github.com/simon/mneme/pkg/events"
)

// Preference represents a learned user preference.
type Preference struct {
	Key        string  `json:"key"`
	Value      string  `json:"value"`
	Source     string  `json:"source"`
	Confidence float64 `json:"confidence"`
}

// Engine performs post-turn reflection and preference extraction.
// Implements agent.ExperienceStore so learned experiences can be
// injected into the agent's context.
//
// By default, Engine stores experiences in-memory. Call UseSQLiteStore
// to enable durable storage with multi-signal similarity search.
//
// When a StabilityDetector is installed via UseFacetSystem, post-turn
// reflections are routed through the candidate buffer → rebuild pipeline
// for decay-based lifecycle management.
type Engine struct {
	mu          sync.RWMutex
	preferences map[string]*Preference
	experiences []agent.Experience
	sqlStore    agent.ExperienceStore // optional SQLite-backed persistent store
	log         *slog.Logger
	provider    inference.Provider // optional — enables LLM-based extraction
	model       string
	eventBus    *events.Bus // optional — publishes KindPreferenceLearned on extraction

	// Stability detector (optional).
	detector  *StabilityDetector
	buffer    *CandidateBuffer
	scheduler *RebuildScheduler
}

func New(log *slog.Logger) *Engine {
	return &Engine{
		preferences: make(map[string]*Preference),
		log:         log,
	}
}

// SetProvider configures an LLM provider for higher-quality preference extraction.
// When set, ReflectWithLLM is used instead of keyword heuristics.
func (e *Engine) SetProvider(p inference.Provider, model string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.provider = p
	e.model = model
}

// SetEventBus configures the event bus for publishing preference learning events.
func (e *Engine) SetEventBus(bus *events.Bus) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.eventBus = bus
}

// UseSQLiteStore configures the engine to use a SQLite-backed persistent store.
// When set, Search and Save delegate to SQLite for durability; the in-memory
// store is kept as a fast-path cache.
func (e *Engine) UseSQLiteStore(store agent.ExperienceStore) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sqlStore = store
}

// Search finds experiences relevant to a query. When a SQLite store is
// configured, it delegates to the multi-signal similarity search. Otherwise
// falls back to in-memory substring matching.
func (e *Engine) Search(ctx context.Context, query string, limit int) ([]agent.Experience, error) {
	e.mu.RLock()
	sqlStore := e.sqlStore
	e.mu.RUnlock()

	if sqlStore != nil {
		return sqlStore.Search(ctx, query, limit)
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(e.experiences) == 0 {
		return nil, nil
	}
	lower := strings.ToLower(query)
	var matches []agent.Experience
	for i := len(e.experiences) - 1; i >= 0 && len(matches) < limit; i-- {
		exp := e.experiences[i]
		if strings.Contains(lower, strings.ToLower(exp.Learning)) ||
			strings.Contains(strings.ToLower(exp.Learning), lower) ||
			strings.Contains(lower, strings.ToLower(exp.Message)) {
			matches = append(matches, exp)
		}
	}
	return matches, nil
}

// Save persists an experience. With SQLite store, writes to durable storage;
// always keeps the in-memory ring buffer as a fast-path cache.
func (e *Engine) Save(ctx context.Context, exp agent.Experience) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if exp.ID == "" {
		exp.ID = uuid.New().String()
	}
	// In-memory ring buffer cache.
	e.experiences = append(e.experiences, exp)
	if len(e.experiences) > 1000 {
		e.experiences = e.experiences[len(e.experiences)-1000:]
	}
	// Durable store if configured.
	if e.sqlStore != nil {
		if err := e.sqlStore.Save(ctx, exp); err != nil {
			e.log.Warn("failed to persist experience to SQLite", "error", err)
		}
	}
	return nil
}

// LastModified returns the time this engine state was last updated.
func (e *Engine) LastModified() time.Time {
	return time.Now() // in-memory only
}

// UseFacetSystem installs the stability-detector-based facet system.
// When set, post-turn reflections push LearningCandidates into the ring buffer
// instead of directly overwriting preferences. The detector rebuild cycle
// then drains the buffer and applies decay-based lifecycle management.
func (e *Engine) UseFacetSystem(cache FacetCache) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.detector = NewStabilityDetector(cache)
	e.buffer = GlobalBuffer()
	e.scheduler = NewRebuildScheduler(e.detector, e.log)
}

// StartFacetRebuildLoop starts the periodic rebuild loop if the facet system is active.
// Call this from the application startup after the context is available.
func (e *Engine) StartFacetRebuildLoop(ctx context.Context) {
	e.mu.RLock()
	s := e.scheduler
	e.mu.RUnlock()
	if s != nil {
		go s.StartPeriodic(ctx)
	}
}

// Shutdown stops background loops and waits for them to finish. Call before
// closing the database to ensure no writes are in flight.
func (e *Engine) Shutdown() {
	e.mu.RLock()
	s := e.scheduler
	e.mu.RUnlock()
	if s != nil {
		s.Stop()
	}
}

// OnFacetTriggerEvent signals the scheduler that a rebuild-triggering event occurred.
func (e *Engine) OnFacetTriggerEvent() {
	e.mu.RLock()
	s := e.scheduler
	e.mu.RUnlock()
	if s != nil {
		s.OnTriggerEvent()
	}
}

// Reflect analyzes a completed conversation turn for learnings.
// Uses LLM-based extraction when a provider is configured, falling back to
// keyword heuristics.
//
// When the facet system is active, extractions are pushed as LearningCandidates
// into the ring buffer for later rebuild instead of directly stored.
func (e *Engine) Reflect(ctx context.Context, threadID, userMsg, assistantMsg string) []*Preference {
	e.mu.RLock()
	hasProvider := e.provider != nil
	hasFacets := e.detector != nil
	e.mu.RUnlock()

	if hasFacets && hasProvider {
		// Route through LLM facet extraction → candidate buffer.
		candidates := e.extractFacets(ctx, userMsg, assistantMsg)
		for _, c := range candidates {
			e.buffer.Push(c)
		}
		if len(candidates) > 0 {
			e.log.Info("facet candidates pushed", "count", len(candidates))
			return nil // successfully extracted facets, skip heuristics
		}
		// LLM extraction failed or found nothing — fall through to heuristics.
		e.log.Debug("facet extraction returned no candidates, falling back to heuristics")
	}

	if hasProvider {
		if prefs := e.reflectWithLLM(ctx, threadID, userMsg, assistantMsg); len(prefs) > 0 {
			return prefs
		}
		// Fall through to heuristics on LLM failure or empty result.
	}
	return e.reflectHeuristic(threadID, userMsg, assistantMsg)
}

// extractFacets uses the LLM to extract LearningCandidates from a conversation turn,
// with evidence_chunks provenance validation.
func (e *Engine) extractFacets(ctx context.Context, userMsg, assistantMsg string) []LearningCandidate {
	e.mu.RLock()
	provider := e.provider
	model := e.model
	e.mu.RUnlock()

	if provider == nil {
		return nil
	}

	prompt := `Analyze this conversation and extract any user preferences as structured facets.

Return a JSON object with a "facets" array. Each facet must include:
- "class": one of [style, identity, tooling, veto, goal, channel]
- "key": short category name (e.g. "communication", "language", "editor")
- "value": the specific preference
- "cue_family": one of [explicit, structural, behavioral, recurrence]
- "evidence_chunks": array of strings — quote the exact text fragments that support this (at least one, non-empty)
- "confidence": 0.0-1.0

Only extract clear, explicit preferences. Do not guess. Return an empty array if nothing is found.

Example:
{"facets": [
  {"class": "tooling", "key": "package_manager", "value": "pnpm", "cue_family": "explicit", "evidence_chunks": ["I always use pnpm for my projects"], "confidence": 0.95},
  {"class": "style", "key": "verbosity", "value": "concise", "cue_family": "behavioral", "evidence_chunks": ["don't need a full explanation"], "confidence": 0.7}
]}

User: ` + userMsg + `
Assistant: ` + assistantMsg

	req := inference.ChatRequest{
		Model:        model,
		SystemPrompt: "You are a structured knowledge extraction system. Return valid JSON only.",
		Messages:     []inference.Message{{Role: "user", Content: prompt}},
		MaxTokens:    1024,
		Temperature:  0.1,
	}

	tokens, errs := provider.Chat(ctx, req)
	var text string
loop:
	for {
		select {
		case tok, ok := <-tokens:
			if !ok {
				break loop
			}
			text += tok.Text
		case err, ok := <-errs:
			if ok && err != nil {
				e.log.Warn("facet extraction LLM failed", "error", err)
				return nil
			}
		case <-ctx.Done():
			return nil
		}
	}

	// Forgiving JSON parse.
	text = strings.TrimSpace(text)
	if start := strings.Index(text, "{"); start >= 0 {
		if end := strings.LastIndex(text, "}"); end > start {
			text = text[start : end+1]
		}
	}

	var result struct {
		Facets []struct {
			Class          string   `json:"class"`
			Key            string   `json:"key"`
			Value          string   `json:"value"`
			CueFamily      string   `json:"cue_family"`
			EvidenceChunks []string `json:"evidence_chunks"`
			Confidence     float64  `json:"confidence"`
		} `json:"facets"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		e.log.Warn("facet JSON parse failed", "error", err)
		return nil
	}

	var candidates []LearningCandidate
	for _, f := range result.Facets {
		// Provenance validation: must have at least one non-empty evidence_chunk.
		if len(f.EvidenceChunks) == 0 || strings.TrimSpace(f.EvidenceChunks[0]) == "" {
			continue
		}
		class, ok := parseFacetClass(f.Class)
		if !ok {
			continue
		}
		cue := parseCueFamily(f.CueFamily)
		conf := f.Confidence
		if conf < 0 {
			conf = 0
		}
		if conf > 1.0 {
			conf = 1.0
		}

		candidates = append(candidates, LearningCandidate{
			Class:             class,
			Key:               f.Key,
			Value:             f.Value,
			CueFamily:         cue,
			Evidence:          EvidenceRef{Type: "DocumentChunk", SourceID: "learning", ChunkID: f.EvidenceChunks[0]},
			InitialConfidence: conf,
		})
	}
	return candidates
}

// reflectWithLLM uses an LLM to extract structured preferences from a conversation turn.
func (e *Engine) reflectWithLLM(ctx context.Context, threadID, userMsg, assistantMsg string) []*Preference {
	e.mu.RLock()
	provider := e.provider
	model := e.model
	e.mu.RUnlock()

	if provider == nil {
		return nil
	}

	extractPrompt := `You are a preference extraction system. Analyze the following conversation turn and extract any user preferences, rules, or patterns the user expressed.

Return a JSON object with a "preferences" array. Each item has: key (category), value (the preference), confidence (0-1). Keep only high-confidence (>0.6) extractions. Return an empty array if nothing clear is found.

Categories: communication_style, tool_preference, format_preference, topic_interest, privacy_boundary, workflow_rule, language_preference, response_length.

Example: {"preferences": [{"key": "communication_style", "value": "concise bullet points", "confidence": 0.9}]}

User message: ` + userMsg + `
Assistant response: ` + assistantMsg

	req := inference.ChatRequest{
		Model:        model,
		SystemPrompt: "You are a structured data extraction system. Always respond with valid JSON only.",
		Messages: []inference.Message{
			{Role: "user", Content: extractPrompt},
		},
		MaxTokens:   512,
		Temperature: 0.1,
	}

	tokens, errs := provider.Chat(ctx, req)
	var responseText string
	for {
		select {
		case tok, ok := <-tokens:
			if !ok {
				goto done
			}
			responseText += tok.Text
		case err, ok := <-errs:
			if ok && err != nil {
				e.log.Warn("learning LLM extraction failed", "error", err)
				return nil
			}
		case <-ctx.Done():
			return nil
		}
	}
done:
	responseText = strings.TrimSpace(responseText)
	if responseText == "" {
		return nil
	}

	// Parse JSON response.
	var result struct {
		Preferences []struct {
			Key        string  `json:"key"`
			Value      string  `json:"value"`
			Confidence float64 `json:"confidence"`
		} `json:"preferences"`
	}
	if err := json.Unmarshal([]byte(responseText), &result); err != nil {
		// Try extracting JSON from markdown code fences.
		if start := strings.Index(responseText, "{"); start >= 0 {
			if end := strings.LastIndex(responseText, "}"); end > start {
				if err := json.Unmarshal([]byte(responseText[start:end+1]), &result); err != nil {
					e.log.Warn("learning JSON parse failed", "error", err)
					return nil
				}
			}
		} else {
			e.log.Warn("learning JSON parse failed", "error", err)
			return nil
		}
	}

	var learned []*Preference
	e.mu.Lock()
	for _, p := range result.Preferences {
		if p.Confidence < 0.6 {
			continue
		}
		pref := &Preference{
			Key:        p.Key,
			Value:      p.Value,
			Source:     threadID,
			Confidence: p.Confidence,
		}
		e.preferences[p.Key] = pref
		learned = append(learned, pref)
	}
	bus := e.eventBus
	e.mu.Unlock()

	for _, p := range learned {
		if bus != nil {
			bus.PublishTyped(events.DomainLearning, events.KindPreferenceLearned, p)
		}
	}
	if len(learned) > 0 {
		e.log.Info("LLM extracted preferences", "count", len(learned))
	}
	return learned
}

// reflectHeuristic extracts preferences using keyword analysis.
// Used as fallback when no LLM provider is configured or extraction fails.
// preferencePattern combines a search phrase with a category key and word-boundary requirement.
type preferencePattern struct {
	phrase         string // lower-case phrase to search for
	category       string // storage key category
	needsBoundary  bool   // require non-alphanumeric char after the match
	needsFollowing bool   // require at least one more word after the match
}

var heuristicPatterns = []preferencePattern{
	// Direct preferences — need word boundary to reject "I preferred..."
	{phrase: "i prefer", category: "explicit_preference", needsBoundary: true, needsFollowing: true},
	{phrase: "i'd prefer", category: "explicit_preference", needsBoundary: true, needsFollowing: true},
	{phrase: "i would prefer", category: "explicit_preference", needsBoundary: true, needsFollowing: true},
	{phrase: "i'd rather", category: "explicit_preference", needsBoundary: true, needsFollowing: true},
	{phrase: "i like", category: "preference", needsBoundary: true, needsFollowing: true},
	{phrase: "i dislike", category: "avoidance", needsBoundary: true},
	{phrase: "i don't like", category: "avoidance", needsBoundary: true},
	{phrase: "i want", category: "goal", needsBoundary: true, needsFollowing: true},
	{phrase: "i need", category: "goal", needsBoundary: true, needsFollowing: true},
	// Habits and instructions
	{phrase: "i always", category: "rule", needsBoundary: true, needsFollowing: true},
	{phrase: "always use", category: "tool_preference", needsBoundary: true, needsFollowing: true},
	{phrase: "never use", category: "rule", needsBoundary: true, needsFollowing: true},
	{phrase: "please always", category: "rule", needsBoundary: true},
	{phrase: "please never", category: "rule", needsBoundary: true},
	{phrase: "please use", category: "tool_preference", needsBoundary: true},
	{phrase: "from now on", category: "rule", needsBoundary: true},
	{phrase: "going forward", category: "rule", needsBoundary: true},
	// Identity and context
	{phrase: "my name is", category: "identity", needsBoundary: true},
	{phrase: "i am a", category: "identity", needsBoundary: true},
	{phrase: "i'm a", category: "identity", needsBoundary: true},
	{phrase: "i work", category: "identity", needsBoundary: true},
	{phrase: "my role", category: "identity", needsBoundary: true},
	{phrase: "my stack", category: "identity", needsBoundary: true},
	{phrase: "my timezone", category: "identity", needsBoundary: true},
	{phrase: "my language", category: "identity", needsBoundary: true},
	{phrase: "my pronouns", category: "identity", needsBoundary: true},
	{phrase: "my preferred", category: "explicit_preference", needsBoundary: true},
	{phrase: "call me", category: "identity", needsBoundary: true},
	{phrase: "address me as", category: "identity", needsBoundary: true},
}

const maxPreferencesPerTurn = 5 // matches Rust MAX_PREFERENCES_PER_TURN

func (e *Engine) reflectHeuristic(threadID, userMsg, assistantMsg string) []*Preference {
	var learned []*Preference
	seen := make(map[string]bool) // prevent duplicate keys per turn

	// Process each sentence separately for word-boundary checking.
	sentences := splitSentences(userMsg)
	for _, sentence := range sentences {
		lowerSentence := strings.ToLower(strings.TrimSpace(sentence))
		if len(lowerSentence) < 10 {
			continue
		}
		for _, pat := range heuristicPatterns {
			idx := strings.Index(lowerSentence, pat.phrase)
			if idx < 0 {
				continue
			}
			// Word boundary: the character after the match must be non-alphanumeric
			// (space, punctuation, end-of-string). Prevents "I preferred" matching "I prefer".
			if pat.needsBoundary {
				endIdx := idx + len(pat.phrase)
				if endIdx < len(lowerSentence) && isAlphanumeric(lowerSentence[endIdx]) {
					continue
				}
			}
			// Following word: at least one more word after the match.
			if pat.needsFollowing {
				rest := strings.TrimSpace(lowerSentence[idx+len(pat.phrase):])
				if len(rest) < 2 {
					continue
				}
			}
			// Extract the value snippet from the sentence (up to 120 chars).
			value := extractSnippet(lowerSentence, idx, 120)
			key := pat.category + "/" + slugify(value[:min(40, len(value))])
			if seen[key] {
				continue
			}
			seen[key] = true

			p := &Preference{
				Key:        key,
				Value:      value,
				Source:     threadID,
				Confidence: 0.7,
			}
			e.mu.Lock()
			e.preferences[key] = p
			bus := e.eventBus
			e.mu.Unlock()
			learned = append(learned, p)
			if bus != nil {
				bus.PublishTyped(events.DomainLearning, events.KindPreferenceLearned, p)
			}
			if len(learned) >= maxPreferencesPerTurn {
				goto done
			}
		}
	}

done:
	if len(learned) > 0 {
		e.log.Info("heuristic preferences learned", "count", len(learned))
	}
	return learned
}

// splitSentences splits text by sentence boundaries.
func splitSentences(text string) []string {
	var sentences []string
	start := 0
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if ch == '.' || ch == '!' || ch == '?' || ch == ';' || ch == '\n' {
			s := strings.TrimSpace(text[start : i+1])
			if len(s) > 0 {
				sentences = append(sentences, s)
			}
			start = i + 1
		}
	}
	if start < len(text) {
		s := strings.TrimSpace(text[start:])
		if len(s) > 0 {
			sentences = append(sentences, s)
		}
	}
	return sentences
}

// extractSnippet returns text[start:] truncated to maxLen chars at a word boundary.
func extractSnippet(text string, start, maxLen int) string {
	end := start + maxLen
	if end > len(text) {
		end = len(text)
	}
	snippet := strings.TrimSpace(text[start:end])
	// Truncate at last space for word boundary.
	if len(snippet) == maxLen && end < len(text) {
		if lastSpace := strings.LastIndexByte(snippet, ' '); lastSpace > 0 {
			snippet = snippet[:lastSpace]
		}
	}
	return snippet
}

// slugify lowercases, replaces spaces with underscores, and strips non-alphanumeric chars.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' {
			b.WriteByte(ch)
		}
	}
	result := b.String()
	if len(result) > 40 {
		result = result[:40]
	}
	return result
}

// Preferences returns all learned preferences.
func (e *Engine) Preferences() []*Preference {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]*Preference, 0, len(e.preferences))
	for _, p := range e.preferences {
		result = append(result, p)
	}
	return result
}

// SubconsciousPrefs returns preferences in the format expected by the
// subconscious.LLMEvaluator (implements subconscious.LearningSnapshot).
func (e *Engine) SubconsciousPrefs() []struct {
	Key        string
	Value      string
	Confidence float64
} {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]struct {
		Key        string
		Value      string
		Confidence float64
	}, 0, len(e.preferences))
	for _, p := range e.preferences {
		out = append(out, struct {
			Key        string
			Value      string
			Confidence float64
		}{p.Key, p.Value, p.Confidence})
	}
	return out
}

func isAlphanumeric(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
