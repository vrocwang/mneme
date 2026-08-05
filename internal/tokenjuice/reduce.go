package tokenjuice

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ── Reduction options ────────────────────────────────────────────────────

// CompactOptions controls how tool output compaction behaves.
type CompactOptions struct {
	MaxInlineChars int     // default 1200
	MaxInputBytes  int     // skip outputs under this size (default 512)
	MinRatio       float64 // don't compact unless ratio <= this (default 0.95)
}

// DefaultCompactOptions returns sensible defaults.
func DefaultCompactOptions() CompactOptions {
	return CompactOptions{
		MaxInlineChars: 1200,
		MaxInputBytes:  512,
		MinRatio:       0.95,
	}
}

// CompactResult holds the outcome of a compaction pass.
type CompactResult struct {
	Text           string           `json:"text"`
	Applied        bool             `json:"applied"`
	RuleID         string           `json:"rule_id,omitempty"`
	Counters       []CompactCounter `json:"counters,omitempty"`
	OriginalBytes  int              `json:"original_bytes"`
	CompactedBytes int              `json:"compacted_bytes"`
}

// CompactCounter reports a named count from rule counter patterns.
type CompactCounter struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// ── Rule loader ──────────────────────────────────────────────────────────

// RuleSet holds the three-layer rule overlay.
type RuleSet struct {
	Builtin []*CompiledRule
	User    []*CompiledRule
	Project []*CompiledRule
}

// LoadRuleSet loads rules from the three-layer overlay.
// userRules and projectRules are JSON-encoded rule arrays ([]byte).
func LoadRuleSet(userRules, projectRules []byte) *RuleSet {
	rs := &RuleSet{
		Builtin: compileRules(loadBuiltinRules()),
	}
	if userRules != nil {
		rs.User = compileRules(parseRules(userRules))
	}
	if projectRules != nil {
		rs.Project = compileRules(parseRules(projectRules))
	}
	return rs
}

func parseRules(data []byte) []*JsonRule {
	var rules []*JsonRule
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil
	}
	return rules
}

// AllRules returns the merged rule list with layering: project > user > builtin.
// Rules with the same ID are overridden. generic/fallback is always last.
func (rs *RuleSet) AllRules() []*CompiledRule {
	merged := make(map[string]*CompiledRule)

	// Lowest priority: builtin
	for _, r := range rs.Builtin {
		merged[r.ID] = r
	}
	// Medium priority: user
	for _, r := range rs.User {
		merged[r.ID] = r
	}
	// Highest priority: project
	for _, r := range rs.Project {
		merged[r.ID] = r
	}

	// Collect and sort
	var all []*CompiledRule
	var fallback *CompiledRule
	for _, r := range merged {
		if r.ID == "generic/fallback" {
			fallback = r
			continue
		}
		all = append(all, r)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].ID < all[j].ID
	})
	if fallback != nil {
		all = append(all, fallback)
	}
	return all
}

// ── Main compaction entry point ──────────────────────────────────────────

// CompactToolOutput applies rule-based compaction to tool output text.
//
// This is the agent-facing wrapper. It:
// 1. Skips outputs too small to benefit from compaction
// 2. Extracts command/argv from tool arguments
// 3. Finds the best rule
// 4. Applies domain tool guard (no generic fallback for non-shell tools)
// 5. Applies the reduction pipeline
// 6. Checks compaction ratio and returns the better text
func CompactToolOutput(output string, toolName string, args map[string]interface{}, rules []*CompiledRule, opts CompactOptions, failed bool) CompactResult {
	if opts.MaxInputBytes <= 0 {
		opts.MaxInputBytes = 512
	}
	if opts.MaxInlineChars <= 0 {
		opts.MaxInlineChars = 1200
	}
	if opts.MinRatio <= 0 {
		opts.MinRatio = 0.95
	}

	originalBytes := len(output)
	result := CompactResult{
		Text:          output,
		OriginalBytes: originalBytes,
	}

	// Skip tiny outputs
	if originalBytes < opts.MaxInputBytes {
		return result
	}

	command, argv := extractCommandArgv(args)
	hasCommand := command != ""

	// Find best matching rule
	rule := findBestRule(rules, toolName, argv)

	result.CompactedBytes = originalBytes

	if rule == nil {
		return result
	}

	// Domain tool guard: allow generic/fallback for shell-like tools, but not for
	// domain tools without a command context (e.g. browser, filesystem).
	if rule.ID == "generic/fallback" && !hasCommand && !isShellLikeTool(toolName) {
		return result
	}

	// Apply the reduction pipeline
	compacted, counters := applyRule(rule, output, failed)

	// Populate counters
	for _, c := range counters {
		result.Counters = append(result.Counters, CompactCounter{Name: c.Name, Count: c.Count})
	}

	// Check that compaction actually helped
	compactedBytes := len(compacted)
	if compactedBytes >= originalBytes {
		return result
	}
	ratio := float64(compactedBytes) / float64(originalBytes)
	if ratio > opts.MinRatio {
		return result
	}

	// Clamp to max inline chars
	compacted = clampText(compacted, opts.MaxInlineChars)

	result.Text = compacted
	result.Applied = true
	result.RuleID = rule.ID
	result.CompactedBytes = len(compacted)
	return result
}

// shellLikeTools lists tool names that should use command/argv matching rules.
func isShellLikeTool(name string) bool {
	switch name {
	case "shell", "bash", "sandbox_cmd", "run_shell", "exec":
		return true
	}
	return false
}

// ── Reduction pipeline ───────────────────────────────────────────────────

// applyRule runs the full reduction pipeline for a matched rule.
// Returns the compacted text and any counters that were matched.
func applyRule(rule *CompiledRule, text string, failed bool) (string, []compiledCounter) {
	// Pretty-print JSON if requested
	if rule.Transforms.PrettyPrintJSON {
		text = prettyPrintJSON(text)
	}

	// Normalize lines (CRLF -> LF, trim trailing whitespace per line)
	lines := normalizeLines(text)

	// Strip ANSI
	if rule.Transforms.StripANSI {
		stripped := make([]string, len(lines))
		for i, line := range lines {
			stripped[i] = StripANSI(line)
		}
		lines = stripped
	}

	// Check output match patterns
	for _, om := range rule.outputCompiled {
		if om.Regex.MatchString(strings.Join(lines, "\n")) {
			return om.Message, nil
		}
	}

	// Save pre-keep lines for counters that need them.
	preKeepLines := lines

	// Apply skip patterns
	lines = applySkipPatterns(lines, rule.skipCompiled)

	// Apply keep patterns
	if len(rule.keepCompiled) > 0 {
		lines = applyKeepPatterns(lines, rule.keepCompiled)
	}

	// Trim empty edges
	if rule.Transforms.TrimEmptyEdges {
		lines = trimEmptyEdges(lines)
	}

	// Dedupe adjacent identical lines
	if rule.Transforms.DedupeAdjacent {
		lines = dedupeAdjacent(lines)
	}

	// Run counters: scan pre-keep or post-keep lines depending on counterSource.
	var matchedCounters []compiledCounter
	for _, cc := range rule.counterCompiled {
		source := preKeepLines
		if cc.Source == "postKeep" {
			source = lines
		}
		count := 0
		for _, line := range source {
			if cc.Regex.MatchString(line) {
				count++
			}
		}
		if count > 0 {
			matchedCounters = append(matchedCounters, compiledCounter{
				Name:  cc.Name,
				Regex: cc.Regex,
				Count: count,
			})
		}
	}

	// Check onEmpty
	if len(lines) == 0 && rule.OnEmpty != "" {
		return rule.OnEmpty, matchedCounters
	}

	// Determine head/tail based on failure state
	head, tail := rule.Summarize.Head, rule.Summarize.Tail
	if failed && rule.Failure != nil && rule.Failure.PreserveOnFail {
		head = rule.Failure.Head
		tail = rule.Failure.Tail
	}

	// Apply head/tail summarization
	text = summarizeHeadTail(lines, head, tail)

	return text, matchedCounters
}

// ── Line processing helpers ──────────────────────────────────────────────

func normalizeLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return lines
}

func applySkipPatterns(lines []string, patterns []*regexp.Regexp) []string {
	if len(patterns) == 0 {
		return lines
	}
	var kept []string
	for _, line := range lines {
		skip := false
		for _, re := range patterns {
			if re.MatchString(line) {
				skip = true
				break
			}
		}
		if !skip {
			kept = append(kept, line)
		}
	}
	return kept
}

func applyKeepPatterns(lines []string, patterns []*regexp.Regexp) []string {
	var kept []string
	for _, line := range lines {
		for _, re := range patterns {
			if re.MatchString(line) {
				kept = append(kept, line)
				break
			}
		}
	}
	if len(kept) == 0 {
		return lines // don't lose everything
	}
	return kept
}

func trimEmptyEdges(lines []string) []string {
	// Trim leading empty lines
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	// Trim trailing empty lines
	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	if start >= end {
		return nil
	}
	return lines[start:end]
}

func dedupeAdjacent(lines []string) []string {
	if len(lines) <= 1 {
		return lines
	}
	result := []string{lines[0]}
	for i := 1; i < len(lines); i++ {
		if lines[i] != lines[i-1] {
			result = append(result, lines[i])
		}
	}
	return result
}

func summarizeHeadTail(lines []string, head, tail int) string {
	total := len(lines)
	if total <= head+tail {
		return strings.Join(lines, "\n")
	}

	headLines := lines[:head]
	tailLines := lines[total-tail:]

	omitted := total - head - tail
	return fmt.Sprintf("%s\n... %d lines omitted ...\n%s",
		strings.Join(headLines, "\n"),
		omitted,
		strings.Join(tailLines, "\n"))
}

// ── Text clamping ────────────────────────────────────────────────────────

// clampText truncates text to maxChars grapheme clusters, trying to break at newlines.
func clampText(text string, maxChars int) string {
	if maxChars <= 0 || len(text) <= maxChars {
		return text
	}

	// For simplicity, use byte-length as a proxy for grapheme count
	// A full grapheme-aware clamp would use unicode segmentation

	// Keep 70% head, 30% tail
	headChars := int(float64(maxChars) * 0.7)
	tailChars := int(float64(maxChars) * 0.3)

	head := text[:min(headChars, len(text))]
	tail := text[max(0, len(text)-tailChars):]

	// Try to break head at last newline
	if idx := strings.LastIndex(head, "\n"); idx > headChars/2 {
		head = head[:idx]
	}
	// Try to break tail at first newline
	if idx := strings.Index(tail, "\n"); idx > 0 && idx < len(tail)/2 {
		tail = tail[idx:]
	}

	omitted := len(text) - len(head) - len(tail)
	return fmt.Sprintf("%s\n... %d bytes omitted ...\n%s", head, omitted, tail)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
