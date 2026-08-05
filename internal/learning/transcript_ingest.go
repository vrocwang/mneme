package learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"github.com/simon/mneme/internal/memory/conversations"
)

// TranscriptIngestor mines completed conversation transcripts for durable
// learning signals (preferences, decisions, commitments, unresolved items).
// Pushes extracted candidates into the LearningCandidate buffer for
// stability detection and eventual persistence.
//
// Matches Rust's transcript_ingest pipeline: extract → dedupe → persist.
type TranscriptIngestor struct {
	store  *conversations.Store
	buffer *CandidateBuffer
	logFn  func(msg string, args ...interface{})

	mu          sync.Mutex
	seenHashes  map[string]bool // content-based deduplication
	concurrency int             // max concurrent persist workers
}

// NewTranscriptIngestor creates a transcript mining pipeline.
func NewTranscriptIngestor(store *conversations.Store, buffer *CandidateBuffer, logFn func(msg string, args ...interface{})) *TranscriptIngestor {
	if logFn == nil {
		logFn = func(msg string, args ...interface{}) {}
	}
	return &TranscriptIngestor{
		store:       store,
		buffer:      buffer,
		logFn:       logFn,
		seenHashes:  make(map[string]bool),
		concurrency: 4,
	}
}

// Phrase lists match Rust transcript_ingest/extract.rs exactly.
// See /data/openhuman/src/legacy/learning/transcript_ingest/extract.rs

var ingestPreferencePhrases = []string{
	"i prefer", "i'd prefer", "i would prefer", "i like",
	"i don't like", "i hate", "i always", "i never",
	"please always", "please don't", "please do not",
	"from now on", "going forward", "i'd rather",
	"i would rather", "i want you to",
}

var ingestDecisionPhrases = []string{
	"let's go with", "let's use", "we'll use", "we will use",
	"i'll use", "i will use", "decided to", "going with",
	"we're going to use", "we picked", "we chose",
}

var ingestCommitmentPhrases = []string{
	"i'll ", "i will ", "i'm going to ", "i am going to ",
	"i plan to ", "i need to ",
}

var ingestUnresolvedPhrases = []string{
	"todo", "still need to", "haven't done", "have not done",
	"not done yet", "still pending", "blocked on",
	"waiting on", "follow up on", "needs follow-up", "next step",
}

var ingestReflectionPhrases = []string{
	"i realized", "i realised", "lesson learned",
	"in hindsight", "next time", "remember that i",
	"remember that we", "we keep ", "we always end up ",
	"this is the second time", "this keeps happening",
}

// Ingest processes all thread messages, extracting candidates from user
// messages and both sides for decisions/commitments.
func (t *TranscriptIngestor) Ingest(ctx context.Context) error {
	threads, err := t.store.ListThreads(1000)
	if err != nil {
		return fmt.Errorf("transcript ingest: list threads: %w", err)
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, t.concurrency)

	for _, th := range threads {
		wg.Add(1)
		go func(threadID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			msgs, err := t.store.GetMessages(threadID, 200)
			if err != nil {
				return
			}
			t.processThread(threadID, msgs)
		}(th.ID)
	}
	wg.Wait()

	t.logFn("transcript ingest complete", "threads", len(threads))
	return nil
}

func (t *TranscriptIngestor) processThread(threadID string, msgs []conversations.Message) {
	// Track recurring preference mentions for reflection (Rust: ≥3 messages).
	prefMentionCount := 0

	for _, msg := range msgs {
		if msg.Role == "tool" || msg.Role == "system" {
			continue
		}
		lower := strings.ToLower(msg.Content)
		if isFiller(lower) {
			continue
		}

		// User-only: preferences and commitments.
		if msg.Role == "user" {
			t.extractCandidates(lower, msg.Content, ingestPreferencePhrases, "preference", threadID, msg.ID)
			t.extractCandidates(lower, msg.Content, ingestCommitmentPhrases, "commitment", threadID, msg.ID)

			// Reflection phrases (matching Rust extract_reflections).
			t.extractCandidates(lower, msg.Content, ingestReflectionPhrases, "reflection", threadID, msg.ID)

			// Count preference mentions for recurring detection.
			for _, p := range ingestPreferencePhrases {
				if strings.Contains(lower, p) {
					prefMentionCount++
					break
				}
			}
		}

		// Both sides: decisions and unresolved tasks.
		t.extractCandidates(lower, msg.Content, ingestDecisionPhrases, "decision", threadID, msg.ID)
		t.extractCandidates(lower, msg.Content, ingestUnresolvedPhrases, "unresolved", threadID, msg.ID)
	}

	// Recurring-preference reflection: ≥3 mentions across user messages.
	if prefMentionCount >= 3 {
		candidate := LearningCandidate{
			Class:             ClassStyle,
			Key:               "recurring_preferences",
			Value:             fmt.Sprintf("User stated personal preferences in %d messages this session — treat as a stable pattern.", prefMentionCount),
			Evidence:          EvidenceRef{Type: "transcript", SourceID: fmt.Sprintf("thread:%s", threadID)},
			CueFamily:         CueBehavioral,
			InitialConfidence: 0.7,
		}
		t.buffer.Push(candidate)
	}
}

func (t *TranscriptIngestor) extractCandidates(lower, original string, phrases []string, cueClass, threadID string, msgID int64) {
	for _, phrase := range phrases {
		idx := strings.Index(lower, phrase)
		if idx < 0 {
			continue
		}
		// Extract sentence from original text (not lowercased) so the
		// stored snippet is human-readable. Matches Rust find_phrase_snippet.
		snippet := extractSentence(original, idx, 400)
		if snippet == "" {
			continue
		}

		hash := contentHash(snippet)
		t.mu.Lock()
		if t.seenHashes[hash] {
			t.mu.Unlock()
			continue
		}
		t.seenHashes[hash] = true
		t.mu.Unlock()

		// Map cue class to importance (matching Rust Importance enum).
		conf := 0.7 // High
		family := CueBehavioral
		if cueClass == "commitment" || cueClass == "unresolved" {
			conf = 0.5 // Medium
		}
		if cueClass == "reflection" {
			family = CueExplicit
		}

		candidate := LearningCandidate{
			Class:             facetClassFromCue(cueClass),
			Key:               slugifyKey(cueClass, snippet),
			Value:             snippet,
			Evidence:          EvidenceRef{Type: "transcript", SourceID: fmt.Sprintf("thread:%s:msg:%d", threadID, msgID)},
			CueFamily:         family,
			InitialConfidence: conf,
		}
		t.buffer.Push(candidate)
	}
}

func facetClassFromCue(cue string) FacetClass {
	switch cue {
	case "preference", "reflection":
		return ClassStyle
	case "decision", "commitment", "unresolved":
		return ClassGoal
	default:
		return ClassStyle
	}
}

func slugifyKey(cue, snippet string) string {
	s := strings.ToLower(cue + "_" + snippet)
	if len(s) > 40 {
		s = s[:40]
	}
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return '_'
	}, s)
}

func contentHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8]) // 16 hex chars is sufficient
}

func extractSentence(text string, start, maxLen int) string {
	// Walk back to sentence start.
	s := start
	for s > 0 && text[s-1] != '.' && text[s-1] != '!' && text[s-1] != '?' && text[s-1] != '\n' {
		s--
	}
	// Walk forward to sentence end or maxLen.
	end := start
	for end < len(text) && end-start < maxLen {
		if text[end] == '.' || text[end] == '!' || text[end] == '?' {
			end++
			break
		}
		end++
	}
	result := strings.TrimSpace(text[s:end])
	if len(result) > maxLen {
		result = result[:maxLen]
	}
	return result
}

func isFiller(text string) bool {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) < 20 {
		return true
	}
	lower := strings.ToLower(trimmed)
	lower = strings.TrimRight(lower, ".!?")
	for _, pat := range []string{"thanks", "thank you", "thx", "ok cool", "sounds good", "got it"} {
		if lower == pat {
			return true
		}
	}
	return false
}
