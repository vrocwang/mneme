package entities

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Kind classifies an entity.
type Kind string

const (
	KindPerson       Kind = "person"
	KindOrganization Kind = "organization"
	KindTopic        Kind = "topic"
	KindProject      Kind = "project"
	KindURL          Kind = "url"
)

// Entity is an extracted entity stored as a markdown file.
type Entity struct {
	Name        string
	Kind        Kind
	Aliases     []string
	Description string
	FilePath    string
	Confidence  float64 `json:"confidence,omitempty"` // extraction confidence 0.0-1.0
}

// Relation represents a subject-predicate-object triple extracted from text.
type Relation struct {
	Subject    string  `json:"subject"`
	Predicate  string  `json:"predicate"`
	Object     string  `json:"object"`
	Confidence float64 `json:"confidence"`
}

// Extraction confidence scores (matching Rust's parse_document scoring).
const (
	ConfEmailEntity      = 0.95
	ConfEmailRelation    = 0.82
	ConfGraphFact        = 0.87
	ConfActionItem       = 0.83
	ConfOwnerRelation    = 0.94
	ConfReviewRelation   = 0.80
	ConfPreference       = 0.90
	ConfCapitalizedTopic = 0.45
	ConfHashtag          = 0.50
	ConfURL              = 0.70

	// DefaultEntityThreshold is the minimum confidence for an entity to be included.
	DefaultEntityThreshold = 0.45
	// DefaultRelationThreshold is the minimum confidence for a relation to be included.
	DefaultRelationThreshold = 0.30
)

// ── Regex patterns (matching Rust's ingestion regex) ────────────────

var (
	// graphFactRe matches "[subject] [predicate] [object]" structured facts.
	// Predicates: works_on, depends_on, uses, evaluates, owns, prefers, reviews, maintains.
	graphFactRe = regexp.MustCompile(`^(?P<subject>[A-Z][a-zA-Z\s]+?)\s+(?P<predicate>works_on|depends_on|uses|evaluates|owns|prefers|reviews|maintains)\s+(?P<object>.+)$`)

	// actionItemRe matches "Name: task description" action items.
	actionItemRe = regexp.MustCompile(`^(?P<subject>[A-Z][a-zA-Z\s]{2,30}):\s*(?P<object>.+)$`)

	// ownerRe matches "[Subject] owns [Object]" ownership declarations.
	ownerRe = regexp.MustCompile(`^(?P<subject>[A-Z][a-zA-Z\s]+?)\s+owns\s+(?P<object>.+)$`)

	// preferenceRe matches "[Subject] prefers [Object]" preference statements.
	preferenceRe = regexp.MustCompile(`^(?P<subject>[A-Z][a-zA-Z\s]+?)\s+prefers\s+(?P<object>.+)$`)

	// personNameRe matches two-word capitalized person names.
	personNameRe = regexp.MustCompile(`\b[A-Z][a-z]+(?: [A-Z][a-z]+)+\b`)
)

// Registry manages entities in a directory tree.
// Layout: <root>/<kind>/<name>.md
type Registry struct {
	mu   sync.RWMutex
	root string
}

// NewRegistry creates an entity registry at the given root.
func NewRegistry(root string) (*Registry, error) {
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("create entity registry dir: %w", err)
	}
	return &Registry{root: root}, nil
}

// Upsert creates or updates an entity.
func (r *Registry) Upsert(e Entity) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	dir := filepath.Join(r.root, string(e.Kind))
	os.MkdirAll(dir, 0755)

	path := filepath.Join(dir, sanitizeFilename(e.Name)+".md")

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("name: %q\n", e.Name))
	b.WriteString(fmt.Sprintf("kind: %s\n", e.Kind))
	b.WriteString(fmt.Sprintf("aliases: [%s]\n", strings.Join(e.Aliases, ", ")))
	b.WriteString("---\n\n")
	b.WriteString(e.Description + "\n")

	return os.WriteFile(path, []byte(b.String()), 0644)
}

// Get retrieves an entity by name and kind.
func (r *Registry) Get(kind Kind, name string) (*Entity, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	path := filepath.Join(r.root, string(kind), sanitizeFilename(name)+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return parseEntityFile(path, string(data)), nil
}

// Search finds entities matching a query in name or description.
func (r *Registry) Search(query string, maxResults int) []Entity {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var results []Entity
	filepath.Walk(r.root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		text := string(data)
		if containsFold(text, query) {
			e := parseEntityFile(path, text)
			results = append(results, *e)
		}
		return nil
	})

	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results
}

// commonCapitalizedStopWords are frequent English words that appear capitalized
// at sentence starts but don't represent named entities.
var commonCapitalizedStopWords = map[string]bool{
	"The": true, "This": true, "That": true, "These": true, "Those": true,
	"And": true, "But": true, "Or": true, "Nor": true, "For": true,
	"With": true, "From": true, "Into": true, "Onto": true, "Upon": true,
	"Have": true, "Has": true, "Had": true, "Not": true, "Are": true,
	"Was": true, "Were": true, "Will": true, "Would": true, "Could": true,
	"Should": true, "Can": true, "May": true, "Might": true, "Shall": true,
	"Each": true, "Every": true, "All": true, "Some": true, "Any": true,
	"More": true, "Most": true, "Other": true, "Such": true, "Only": true,
	"Just": true, "Also": true, "Still": true, "Then": true, "Now": true,
	"Here": true, "There": true, "It": true, "Its": true, "He": true,
	"She": true, "They": true, "We": true, "You": true, "Your": true,
	"My": true, "Our": true, "Their": true, "His": true, "Her": true,
	"To": true, "In": true, "On": true, "At": true, "By": true,
	"Of": true, "About": true, "Between": true, "Through": true, "During": true,
	"Before": true, "After": true, "Above": true, "Below": true, "Under": true,
	"Over": true, "Again": true, "Once": true, "Very": true, "Too": true,
	"So": true, "If": true, "Than": true, "As": true, "Like": true,
	"When": true, "Where": true, "Which": true, "What": true, "Who": true,
	"How": true, "Why": true, "Whose": true,
}

// ExtractFromText performs keyword and pattern-based entity extraction with
// confidence scoring. Detects emails, person names, URLs, hashtags, @mentions,
// org domains, graph facts, and capitalized topics. Entities below
// DefaultEntityThreshold are filtered out.
func ExtractFromText(text string) []Entity {
	var entities []Entity
	seen := make(map[string]bool)

	// Email addresses — high confidence.
	emailRe := regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	for _, match := range emailRe.FindAllString(text, -1) {
		if !seen[match] {
			seen[match] = true
			entities = append(entities, Entity{Name: match, Kind: KindPerson, Confidence: ConfEmailEntity})
		}
	}

	// Person names (two-word capitalized patterns).
	for _, match := range personNameRe.FindAllString(text, -1) {
		name := strings.TrimSpace(match)
		if !seen[name] {
			seen[name] = true
			entities = append(entities, Entity{Name: name, Kind: KindPerson, Confidence: 0.75})
		}
	}

	// Graph fact subjects and objects — extract entities from S-P-O lines.
	for _, m := range graphFactRe.FindAllStringSubmatch(text, -1) {
		for _, name := range []string{strings.TrimSpace(m[1]), strings.TrimSpace(m[3])} {
			if len(name) > 2 && !seen[name] && !commonCapitalizedStopWords[name] {
				seen[name] = true
				entities = append(entities, Entity{Name: name, Kind: KindTopic, Confidence: ConfGraphFact})
			}
		}
	}

	// Action item subjects.
	for _, m := range actionItemRe.FindAllStringSubmatch(text, -1) {
		name := strings.TrimSpace(m[1])
		if len(name) > 1 && !seen[name] && !commonCapitalizedStopWords[name] {
			seen[name] = true
			entities = append(entities, Entity{Name: name, Kind: KindPerson, Confidence: ConfActionItem})
		}
	}

	// Hashtags.
	hashRe := regexp.MustCompile(`#\w{2,}`)
	for _, match := range hashRe.FindAllString(text, -1) {
		name := strings.TrimPrefix(match, "#")
		if !seen[name] {
			seen[name] = true
			entities = append(entities, Entity{Name: name, Kind: KindTopic, Confidence: ConfHashtag})
		}
	}

	patterns := map[string]Kind{
		"@":    KindPerson,
		"http": KindURL,
		".com": KindOrganization,
		".org": KindOrganization,
	}

	words := strings.Fields(text)

	for _, word := range words {
		for prefix, kind := range patterns {
			if strings.HasPrefix(word, prefix) || strings.Contains(word, prefix) {
				name := strings.TrimRight(strings.TrimLeft(word, "@#"), ",.!?;:\"')]")
				if len(name) > 2 && !seen[name] {
					seen[name] = true
					conf := 0.60
					if kind == KindURL {
						conf = ConfURL
					}
					entities = append(entities, Entity{
						Name:       name,
						Kind:       kind,
						Confidence: conf,
					})
				}
				break
			}
		}

		// Only extract capitalized words as potential topics if they pass
		// the stop-word filter and are at least 4 characters.
		if isCapitalized(word) && len(word) >= 4 && !seen[word] && !commonCapitalizedStopWords[word] {
			seen[word] = true
			entities = append(entities, Entity{
				Name:       word,
				Kind:       KindTopic,
				Confidence: ConfCapitalizedTopic,
			})
		}
	}

	// Filter by confidence threshold.
	return filterByConfidence(entities, DefaultEntityThreshold)
}

// ExtractRelations extracts subject-predicate-object relations from text
// using graph fact, owner, preference, and action item regex patterns.
// Relations below DefaultRelationThreshold are filtered out.
func ExtractRelations(text string) []Relation {
	var relations []Relation

	// Graph facts: "subject works_on/owns/prefers/etc object"
	for _, m := range graphFactRe.FindAllStringSubmatch(text, -1) {
		relations = append(relations, Relation{
			Subject:    strings.TrimSpace(m[1]),
			Predicate:  strings.TrimSpace(m[2]),
			Object:     strings.TrimSpace(m[3]),
			Confidence: ConfGraphFact,
		})
	}

	// Ownership: "Subject owns Object"
	for _, m := range ownerRe.FindAllStringSubmatch(text, -1) {
		relations = append(relations, Relation{
			Subject:    strings.TrimSpace(m[1]),
			Predicate:  "owns",
			Object:     strings.TrimSpace(m[2]),
			Confidence: ConfOwnerRelation,
		})
	}

	// Preferences: "Subject prefers Object"
	for _, m := range preferenceRe.FindAllStringSubmatch(text, -1) {
		relations = append(relations, Relation{
			Subject:    strings.TrimSpace(m[1]),
			Predicate:  "prefers",
			Object:     strings.TrimSpace(m[2]),
			Confidence: ConfPreference,
		})
	}

	// Action items: "Name: task"
	for _, m := range actionItemRe.FindAllStringSubmatch(text, -1) {
		obj := strings.TrimSpace(m[2])
		if len(obj) > 2 {
			relations = append(relations, Relation{
				Subject:    strings.TrimSpace(m[1]),
				Predicate:  "works_on",
				Object:     obj,
				Confidence: ConfActionItem,
			})
		}
	}

	// Filter by confidence threshold.
	return filterRelations(relations, DefaultRelationThreshold)
}

func filterByConfidence(entities []Entity, threshold float64) []Entity {
	out := make([]Entity, 0, len(entities))
	for _, e := range entities {
		if e.Confidence >= threshold {
			out = append(out, e)
		}
	}
	return out
}

func filterRelations(relations []Relation, threshold float64) []Relation {
	out := make([]Relation, 0, len(relations))
	for _, r := range relations {
		if r.Confidence >= threshold {
			out = append(out, r)
		}
	}
	return out
}

// Enricher can refine entities extracted by regex with richer descriptions,
// classifications, and aliases. Implementations may use an LLM or external
// knowledge base. When nil, regex extraction alone is used.
type Enricher interface {
	Enrich(ctx context.Context, entities []Entity) ([]Entity, error)
}

// LLMEnricher uses an LLM to add descriptions and classify entities.
type LLMEnricher struct {
	call func(ctx context.Context, prompt string) (string, error)
}

// NewLLMEnricher creates an entity enricher backed by an LLM call function.
// The call function should send the prompt to the LLM and return the response.
func NewLLMEnricher(call func(ctx context.Context, prompt string) (string, error)) *LLMEnricher {
	return &LLMEnricher{call: call}
}

// Enrich sends entities to the LLM for enrichment: adding descriptions,
// correcting kinds, and suggesting aliases. Falls back to original entities
// on any error.
func (e *LLMEnricher) Enrich(ctx context.Context, entities []Entity) ([]Entity, error) {
	if e.call == nil || len(entities) == 0 {
		return entities, nil
	}

	var names []string
	for _, ent := range entities {
		names = append(names, ent.Name)
	}

	prompt := fmt.Sprintf(
		`Given these named entities extracted from text: %s.

For each entity, return a JSON array of objects with: "name" (the entity name), "kind" (one of: person, organization, topic, project, url), "description" (1-sentence max), "aliases" (array of alternative names, max 2). Keep descriptions factual and brief. Return ONLY the JSON array.`,
		strings.Join(names, ", "),
	)

	resp, err := e.call(ctx, prompt)
	if err != nil {
		return entities, nil // fallback: original entities unchanged
	}

	// Parse JSON array response.
	enriched, err := parseEnrichedEntities(resp)
	if err != nil || len(enriched) == 0 {
		return entities, nil
	}

	// Merge: update original entities with LLM output where names match.
	byName := make(map[string]*Entity, len(entities))
	for i := range entities {
		byName[strings.ToLower(entities[i].Name)] = &entities[i]
	}
	for _, ee := range enriched {
		if orig, ok := byName[strings.ToLower(ee.Name)]; ok {
			if ee.Kind != "" {
				orig.Kind = ee.Kind
			}
			if ee.Description != "" {
				orig.Description = ee.Description
			}
			if len(ee.Aliases) > 0 {
				orig.Aliases = append(orig.Aliases, ee.Aliases...)
			}
		}
	}

	return entities, nil
}

// parseEnrichedEntities extracts a JSON array of Entity from LLM output.
func parseEnrichedEntities(raw string) ([]Entity, error) {
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start < 0 || end <= start {
		// Try JSON object with "entities" key.
		start = strings.Index(raw, "{")
		end = strings.LastIndex(raw, "}")
		if start < 0 || end <= start {
			return nil, fmt.Errorf("no JSON array or object found in enriched response")
		}
	}
	jsonStr := raw[start : end+1]
	// Try direct entity array first.
	var entities []Entity
	if err := jsonUnmarshal([]byte(jsonStr), &entities); err == nil && len(entities) > 0 {
		return entities, nil
	}
	return nil, fmt.Errorf("parse enriched entities failed")
}

// jsonUnmarshal delegates to encoding/json.
func jsonUnmarshal(data []byte, v interface{}) error { return json.Unmarshal(data, v) }

func isCapitalized(s string) bool {
	if len(s) == 0 {
		return false
	}
	return s[0] >= 'A' && s[0] <= 'Z'
}

func sanitizeFilename(name string) string {
	r := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_",
		"?", "_", "\"", "_", "<", "_", ">", "_", "|", "_",
		" ", "_",
	)
	return strings.ToLower(r.Replace(name))
}

func parseEntityFile(path string, content string) *Entity {
	e := &Entity{FilePath: path}
	// Simple YAML frontmatter parser
	if strings.HasPrefix(content, "---\n") {
		end := strings.Index(content[4:], "\n---")
		if end > 0 {
			fm := content[4 : end+4]
			for _, line := range strings.Split(fm, "\n") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) != 2 {
					continue
				}
				key := strings.TrimSpace(parts[0])
				val := strings.TrimSpace(parts[1])
				switch key {
				case "name":
					e.Name = strings.Trim(val, `"`)
				case "kind":
					e.Kind = Kind(strings.Trim(val, `"`))
				case "aliases":
					val = strings.Trim(val, "[]")
					for _, a := range strings.Split(val, ",") {
						e.Aliases = append(e.Aliases, strings.TrimSpace(a))
					}
				}
			}
			// Description is everything after frontmatter
			if end+8 < len(content) {
				e.Description = strings.TrimSpace(content[end+8:])
			}
		}
	}
	return e
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
