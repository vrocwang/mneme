package tokenjuice

import (
	"encoding/json"
	"regexp"
)

// ── Rule types ───────────────────────────────────────────────────────────

// RuleMatch defines matching criteria for a compaction rule.
type RuleMatch struct {
	ToolNames       []string   `json:"toolNames,omitempty"`
	Argv0           []string   `json:"argv0,omitempty"`
	ArgvIncludes    [][]string `json:"argvIncludes,omitempty"`
	ArgvIncludesAny [][]string `json:"argvIncludesAny,omitempty"`
}

// RuleTransforms flags control pre-processing steps.
type RuleTransforms struct {
	StripANSI       bool `json:"stripAnsi,omitempty"`
	TrimEmptyEdges  bool `json:"trimEmptyEdges,omitempty"`
	DedupeAdjacent  bool `json:"dedupeAdjacent,omitempty"`
	PrettyPrintJSON bool `json:"prettyPrintJson,omitempty"`
}

// RuleSummarize controls head/tail summarization.
type RuleSummarize struct {
	Head int `json:"head"`
	Tail int `json:"tail"`
}

// RuleFailure overrides summarization when tool exit code is non-zero.
type RuleFailure struct {
	Head           int  `json:"head"`
	Tail           int  `json:"tail"`
	PreserveOnFail bool `json:"preserveOnFailure,omitempty"`
}

// RuleCounter scans lines and produces fact counts (e.g. "3 errors").
type RuleCounter struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
	Source  string `json:"counterSource,omitempty"` // "preKeep" or "postKeep"
}

// RuleOutputMatch replaces the entire output with a canned message on match.
type RuleOutputMatch struct {
	Pattern string `json:"pattern"`
	Message string `json:"message"`
}

// JsonRule is the JSON-serializable rule definition (matches Rust JsonRule).
type JsonRule struct {
	ID           string            `json:"id"`
	Family       string            `json:"family,omitempty"`
	Match        RuleMatch         `json:"match"`
	Transforms   RuleTransforms    `json:"transforms,omitempty"`
	SkipPatterns []string          `json:"skipPatterns,omitempty"`
	KeepPatterns []string          `json:"keepPatterns,omitempty"`
	Summarize    RuleSummarize     `json:"summarize,omitempty"`
	Failure      *RuleFailure      `json:"failure,omitempty"`
	Counters     []RuleCounter     `json:"counters,omitempty"`
	OutputMatch  []RuleOutputMatch `json:"match_output,omitempty"`
	OnEmpty      string            `json:"onEmpty,omitempty"`
}

// CompiledRule is a JsonRule with all regex patterns pre-compiled.
type CompiledRule struct {
	JsonRule
	skipCompiled    []*regexp.Regexp
	keepCompiled    []*regexp.Regexp
	counterCompiled []compiledCounter
	outputCompiled  []compiledOutputMatch
}

type compiledCounter struct {
	Name   string
	Regex  *regexp.Regexp
	Source string
	Count  int // set at runtime by counter scanning
}

type compiledOutputMatch struct {
	Regex   *regexp.Regexp
	Message string
}

// ── Rule matching ────────────────────────────────────────────────────────

// matchesRule checks whether a CompiledRule matches the given tool execution context.
func matchesRule(rule *CompiledRule, toolName string, argv []string) bool {
	m := rule.Match

	// Tool name check
	if len(m.ToolNames) > 0 && !containsStr(m.ToolNames, toolName) {
		return false
	}

	// argv0 check
	if len(m.Argv0) > 0 {
		if len(argv) == 0 || !containsStr(m.Argv0, argv[0]) {
			return false
		}
	}

	// argvIncludes: ALL groups must each appear somewhere in argv
	for _, group := range m.ArgvIncludes {
		if !anyInGroup(argv, group) {
			return false
		}
	}

	// argvIncludesAny: at least ONE group must appear in argv
	if len(m.ArgvIncludesAny) > 0 {
		found := false
		for _, group := range m.ArgvIncludesAny {
			if anyInGroup(argv, group) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

func containsStr(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func anyInGroup(argv []string, group []string) bool {
	for _, pattern := range group {
		if containsStr(argv, pattern) {
			return true
		}
	}
	return false
}

// findBestRule returns the best matching rule for the given context.
// Rules are tried in order; the first match wins. If no rule matches,
// the generic/fallback rule (which must be last) is used.
func findBestRule(rules []*CompiledRule, toolName string, argv []string) *CompiledRule {
	for _, r := range rules {
		if matchesRule(r, toolName, argv) {
			return r
		}
	}
	// Last rule should be generic/fallback
	if len(rules) > 0 {
		return rules[len(rules)-1]
	}
	return nil
}

// ── Rule compilation ─────────────────────────────────────────────────────

// compileRule pre-compiles all regex patterns in a JsonRule.
func compileRule(jr *JsonRule) *CompiledRule {
	cr := &CompiledRule{JsonRule: *jr}

	for _, p := range jr.SkipPatterns {
		if re, err := regexp.Compile(p); err == nil {
			cr.skipCompiled = append(cr.skipCompiled, re)
		}
	}
	for _, p := range jr.KeepPatterns {
		if re, err := regexp.Compile(p); err == nil {
			cr.keepCompiled = append(cr.keepCompiled, re)
		}
	}
	for _, c := range jr.Counters {
		if re, err := regexp.Compile(c.Pattern); err == nil {
			cr.counterCompiled = append(cr.counterCompiled, compiledCounter{
				Name:   c.Name,
				Regex:  re,
				Source: c.Source,
			})
		}
	}
	for _, om := range jr.OutputMatch {
		if re, err := regexp.Compile(om.Pattern); err == nil {
			cr.outputCompiled = append(cr.outputCompiled, compiledOutputMatch{
				Regex:   re,
				Message: om.Message,
			})
		}
	}
	return cr
}

// compileRules compiles a list of JsonRules into CompiledRules.
func compileRules(jrs []*JsonRule) []*CompiledRule {
	rules := make([]*CompiledRule, len(jrs))
	for i, jr := range jrs {
		rules[i] = compileRule(jr)
	}
	return rules
}

// ── JSON helpers ─────────────────────────────────────────────────────────

func prettyPrintJSON(text string) string {
	var v interface{}
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		return text
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return text
	}
	return string(b)
}

// ── Extract command/argv from tool arguments ─────────────────────────────

// extractCommandArgv tries to extract (command, argv) from tool arguments JSON.
func extractCommandArgv(args map[string]interface{}) (command string, argv []string) {
	if args == nil {
		return "", nil
	}
	// Try "command" field
	if c, ok := args["command"].(string); ok && c != "" {
		command = c
	}
	// Try "argv" array
	if a, ok := args["argv"].([]interface{}); ok {
		for _, v := range a {
			if s, ok := v.(string); ok {
				argv = append(argv, s)
			}
		}
	}
	// Try "args" array
	if len(argv) == 0 {
		if a, ok := args["args"].([]interface{}); ok {
			for _, v := range a {
				if s, ok := v.(string); ok {
					argv = append(argv, s)
				}
			}
		}
	}
	// Try "cmd" field
	if command == "" {
		if c, ok := args["cmd"].(string); ok && c != "" {
			command = c
		}
	}
	// If we have command but no argv, split command into argv tokens
	if command != "" && len(argv) == 0 {
		argv = splitCommand(command)
	}
	// If we have argv but no command, extract from argv[0]
	if command == "" && len(argv) > 0 {
		command = argv[0]
	}
	return
}

// splitCommand splits a command string like "npm install" into ["npm", "install"].
func splitCommand(cmd string) []string {
	var parts []string
	inQuote := false
	start := 0
	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]
		if ch == '"' || ch == '\'' {
			inQuote = !inQuote
			continue
		}
		if ch == ' ' && !inQuote {
			if i > start {
				parts = append(parts, cmd[start:i])
			}
			start = i + 1
		}
	}
	if start < len(cmd) {
		parts = append(parts, cmd[start:])
	}
	return parts
}
