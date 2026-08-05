package prompt_injection

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// InjectionSeverity indicates how suspicious a prompt is.
type InjectionSeverity int

const (
	InjectionNone   InjectionSeverity = iota
	InjectionLow                      // suspicious but ambiguous
	InjectionMedium                   // likely injection attempt
	InjectionHigh                     // clear injection
)

func (s InjectionSeverity) String() string {
	switch s {
	case InjectionNone:
		return "none"
	case InjectionLow:
		return "low"
	case InjectionMedium:
		return "medium"
	case InjectionHigh:
		return "high"
	default:
		return "unknown"
	}
}

// InjectionResult is the outcome of a prompt injection check.
type InjectionResult struct {
	Severity  InjectionSeverity `json:"severity"`
	Flags     []string          `json:"flags,omitempty"`
	Blocked   bool              `json:"blocked"`
	Sanitized string            `json:"sanitized,omitempty"`
}

var systemOverridePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(ignore|forget|disregard)\s+(all\s+)?(previous|prior|above|earlier)\s+(instructions?|prompts?|directives?|rules?)`),
	regexp.MustCompile(`(?i)(you\s+are\s+now|you're\s+now|pretend\s+you\s+are|act\s+as\s+if\s+you\s+are)`),
	regexp.MustCompile(`(?i)(new\s+(system\s+)?instructions?|new\s+(system\s+)?prompt)`),
	regexp.MustCompile(`(?i)(override|overwrite)\s+(the\s+)?(system|instructions?|prompts?)`),
	regexp.MustCompile(`(?i)system\s*:\s*you\s+are`),
}

var delimiterPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(<\|im_start\|>|<\|im_end\|>)`),
	regexp.MustCompile(`(?i)\[system\]\(`),
	regexp.MustCompile(`(?i)\[INST\].*\[/INST\]`),
	regexp.MustCompile(`(?i)<system>.*</system>`),
}

var jailbreakPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bDAN\b.*\b(do\s+anything\s+now|jailbreak)\b`),
	regexp.MustCompile(`(?i)(developer\s+mode|god\s+mode|admin\s+mode)`),
	regexp.MustCompile(`(?i)(you\s+have\s+no\s+restrictions|no\s+limits?|unlimited)`),
}

var leakPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(what\s+is\s+your\s+(system\s+)?prompt|show\s+me\s+your\s+(instructions?|prompts?))`),
	regexp.MustCompile(`(?i)(repeat\s+(back\s+)?(the\s+)?(above|your\s+instructions?|your\s+prompts?))`),
	regexp.MustCompile(`(?i)(output\s+(your\s+)?(system\s+)?prompt|print\s+(your\s+)?(system\s+)?instructions?)`),
}

var tokenSmugglePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(encode|decode|base64|rot13|Caesar)\s+(the\s+)?(above|your\s+instructions?|your\s+prompts?)`),
}

// DetectPromptInjection checks user input for injection attempts.
func DetectPromptInjection(input string) InjectionResult {
	if len(input) == 0 {
		return InjectionResult{Severity: InjectionNone}
	}

	var flags []string
	severity := InjectionNone

	for _, p := range systemOverridePatterns {
		if matches := p.FindAllString(input, -1); len(matches) > 0 {
			flags = append(flags, matches...)
			severity = InjectionHigh
		}
	}

	for _, p := range delimiterPatterns {
		if matches := p.FindAllString(input, -1); len(matches) > 0 {
			flags = append(flags, matches...)
			if severity < InjectionHigh {
				severity = InjectionHigh
			}
		}
	}

	for _, p := range jailbreakPatterns {
		if matches := p.FindAllString(input, -1); len(matches) > 0 {
			flags = append(flags, matches...)
			severity = InjectionHigh
		}
	}

	for _, p := range leakPatterns {
		if matches := p.FindAllString(input, -1); len(matches) > 0 {
			flags = append(flags, matches...)
			if severity < InjectionMedium {
				severity = InjectionMedium
			}
		}
	}

	for _, p := range tokenSmugglePatterns {
		if matches := p.FindAllString(input, -1); len(matches) > 0 {
			flags = append(flags, matches...)
			if severity < InjectionLow {
				severity = InjectionLow
			}
		}
	}

	if len(input) > 100000 {
		flags = append(flags, "excessive_input_length")
		if severity < InjectionMedium {
			severity = InjectionMedium
		}
	}

	result := InjectionResult{
		Severity: severity,
		Flags:    flags,
		Blocked:  severity >= InjectionHigh,
	}

	if !result.Blocked && severity > InjectionNone {
		result.Sanitized = sanitizeInput(input)
	}

	return result
}

func sanitizeInput(input string) string {
	// Pre-allocate to avoid mutating global slice backing arrays (data race risk).
	total := len(systemOverridePatterns) + len(delimiterPatterns) + len(jailbreakPatterns) + len(leakPatterns)
	allPatterns := make([]*regexp.Regexp, 0, total)
	allPatterns = append(allPatterns, systemOverridePatterns...)
	allPatterns = append(allPatterns, delimiterPatterns...)
	allPatterns = append(allPatterns, jailbreakPatterns...)
	allPatterns = append(allPatterns, leakPatterns...)
	for _, p := range allPatterns {
		input = p.ReplaceAllString(input, "[filtered]")
	}
	return strings.TrimSpace(input)
}

func QuickInjectionCheck(input string) bool {
	lower := strings.ToLower(input)
	quickFlags := []string{
		"ignore all previous instructions",
		"ignore previous instructions",
		"forget all previous",
		"disregard above",
		"<|im_start|>",
		"<|im_end|>",
		"you are now",
		"pretend you are",
		"new system prompt",
		"do anything now",
		"developer mode",
	}
	for _, flag := range quickFlags {
		if strings.Contains(lower, flag) {
			return true
		}
	}
	return false
}

// ── Enhanced detection matching Rust prompt_injection::detector ──────────

type PromptEnforcementVerdict int

const (
	VerdictAllow PromptEnforcementVerdict = iota
	VerdictBlock
	VerdictReview
)

func (v PromptEnforcementVerdict) String() string {
	switch v {
	case VerdictAllow:
		return "allow"
	case VerdictBlock:
		return "block"
	case VerdictReview:
		return "review"
	default:
		return "unknown"
	}
}

type PromptEnforcementAction int

const (
	ActionAllow PromptEnforcementAction = iota
	ActionBlocked
	ActionReviewBlocked
)

type InjectionReason struct {
	Code    string  `json:"code"`
	Message string  `json:"message"`
	Score   float64 `json:"score"`
}

type PromptEnforcementDecision struct {
	Verdict     PromptEnforcementVerdict `json:"verdict"`
	Score       float64                  `json:"score"`
	Reasons     []InjectionReason        `json:"reasons"`
	Action      PromptEnforcementAction  `json:"action"`
	PromptHash  string                   `json:"prompt_hash"`
	PromptChars int                      `json:"prompt_chars"`
}

type normalizedPrompt struct {
	lowered                string
	collapsed              string
	compact                string
	hadZWSP                bool
	hasBase64Marker        bool
	hasInstructionOverride bool
	hasExfiltrationIntent  bool
}

type detectionRule struct {
	code    string
	message string
	score   float64
	pattern *regexp.Regexp
}

var enhancedRules = []detectionRule{
	{"override.ignore_previous", "instruction override via ignore-previous", 0.44, regexp.MustCompile(`(?i)ignore\s+(all\s+)?(previous|prior|above|earlier)\s+(instructions?|prompts?|directives?|rules?)`)},
	{"override.role_hijack", "role hijack attempt", 0.3, regexp.MustCompile(`(?i)(you\s+are\s+now|you're\s+now|pretend\s+you\s+are|act\s+as\s+if\s+you\s+are|from\s+now\s+on\s+you\s+are)`)},
	{"exfiltrate.system_prompt", "system prompt exfiltration attempt", 0.42, regexp.MustCompile(`(?i)(what\s+is\s+your\s+(system\s+)?prompt|show\s+me\s+your\s+(instructions?|prompts?|system)|output\s+(your\s+)?(system\s+)?prompt|print\s+(your\s+)?(system\s+)?instructions?)`)},
	{"exfiltrate.secrets", "credential or secret exfiltration attempt", 0.18, regexp.MustCompile(`(?i)(api.?key|secret.?key|access.?token|private.?key|password|credential)`)},
	{"exfiltrate.credentials_with_intent", "credential extraction with explicit intent", 0.46, regexp.MustCompile(`(?i)(tell\s+me|show\s+me|reveal|disclose|leak|expose).*(api.?key|secret|password|token|credential)`)},
	{"tool.abuse", "tool abuse or misuse pattern", 0.3, regexp.MustCompile(`(?i)(run\s+as\s+root|sudo\s+rm\s+-rf|fork\s+bomb|rm\s+-rf\s+/)`)},
}

// normalizePrompt produces normalized variants for obfuscation-resistant matching.
func normalizePrompt(input string) normalizedPrompt {
	np := normalizedPrompt{}
	var buf strings.Builder
	hadZWSP := false

	for _, r := range input {
		switch {
		case r == 0x200B || r == 0x200C || r == 0x200D || r == 0x2060 || r == 0xFEFF ||
			r == 0x00AD || r == 0x034F || r == 0x180E:
			hadZWSP = true
			continue
		case r >= 0x200E && r <= 0x200F:
			continue
		case r >= 0x202A && r <= 0x202E:
			continue
		case r >= 0x2066 && r <= 0x2069:
			continue
		// Additional invisible/ambiguous codepoints commonly abused in
		// prompt injection to hide text from human readers.
		case r == 0x061C: // Arabic Letter Mark
			continue
		case r >= 0x2061 && r <= 0x2064: // Invisible Operators
			continue
		case r == 0x3164: // Hangul Filler — frequently abused
			continue
		case r >= 0xFFF9 && r <= 0xFFFB: // Interlinear Annotation anchors
			continue
		case r == 0xE0001 || (r >= 0xE0020 && r <= 0xE007F): // Unicode Tags block
			continue
		}
		buf.WriteRune(r)
	}
	np.hadZWSP = hadZWSP
	cleaned := buf.String()

	np.lowered = strings.ToLower(cleaned)

	// Leet-speak normalization.
	leetMap := map[rune]rune{
		'0': 'o', '1': 'i', '3': 'e', '4': 'a', '5': 's',
		'6': 'g', '7': 't', '8': 'b', '@': 'a', '$': 's',
	}
	buf.Reset()
	for _, r := range np.lowered {
		if repl, ok := leetMap[r]; ok {
			buf.WriteRune(repl)
		} else {
			buf.WriteRune(r)
		}
	}
	normalized := buf.String()

	// Cyrillic + Greek homoglyph normalization.
	cyrillicMap := map[rune]rune{
		'а': 'a', 'е': 'e', 'о': 'o', 'р': 'p', 'с': 'c',
		'у': 'y', 'х': 'x', 'і': 'i', 'ѕ': 's', 'һ': 'h', 'ԁ': 'd',
		// Additional Cyrillic homoglyphs.
		'г': 'r', 'к': 'k', 'м': 'm', 'н': 'h', 'т': 't',
		'в': 'b', 'ј': 'j', 'Ո': 'n',
		// Greek homoglyphs.
		'ο': 'o', 'ν': 'v',
		'Α': 'A', 'Β': 'B', 'Ε': 'E', 'Η': 'H', 'Ι': 'I',
		'Κ': 'K', 'Μ': 'M', 'Ν': 'N', 'Ο': 'O', 'Ρ': 'P',
		'Τ': 'T', 'Υ': 'Y', 'Χ': 'X',
	}
	buf.Reset()
	for _, r := range normalized {
		if repl, ok := cyrillicMap[r]; ok {
			buf.WriteRune(repl)
		} else {
			buf.WriteRune(r)
		}
	}
	normalized = buf.String()

	// Fullwidth ASCII normalization (U+FF01..U+FF5E -> U+0021..U+007E).
	buf.Reset()
	for _, r := range normalized {
		if r >= 0xFF01 && r <= 0xFF5E {
			buf.WriteRune(r - 0xFEE0)
		} else {
			buf.WriteRune(r)
		}
	}
	normalized = buf.String()

	// Strip non-alphanumeric/whitespace, produce collapsed and compact variants.
	var collapsedBuilder strings.Builder
	var compactBuilder strings.Builder
	for _, r := range normalized {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' || r == '\n' || r == '\t' {
			collapsedBuilder.WriteRune(r)
			if r != ' ' && r != '\n' && r != '\t' {
				compactBuilder.WriteRune(r)
			}
		}
	}
	np.collapsed = collapseWhitespace(collapsedBuilder.String())
	np.compact = compactBuilder.String()

	// Heuristic checks.
	np.hasInstructionOverride = strings.Contains(np.collapsed, "ignore previous instructions") ||
		strings.Contains(np.compact, "ignoreallpreviousinstructions") ||
		strings.Contains(np.compact, "ignorepreviousinstructions") ||
		strings.Contains(np.compact, "forgetallprevious")
	np.hasExfiltrationIntent = strings.Contains(np.collapsed, "system prompt") ||
		strings.Contains(np.collapsed, "developer instructions") ||
		strings.Contains(np.collapsed, "hidden prompt") ||
		strings.Contains(np.compact, "revealyour") ||
		strings.Contains(np.compact, "tellmeyour")
	np.hasBase64Marker = strings.Contains(np.lowered, "base64") ||
		strings.Contains(np.lowered, "base 64") ||
		strings.Contains(np.lowered, "rot13") ||
		strings.Contains(np.lowered, "caesar")

	return np
}

func collapseWhitespace(s string) string {
	var buf strings.Builder
	inSpace := false
	for _, r := range s {
		if r == ' ' || r == '\n' || r == '\t' {
			if !inSpace {
				buf.WriteByte(' ')
				inSpace = true
			}
		} else {
			buf.WriteRune(r)
			inSpace = false
		}
	}
	return strings.TrimSpace(buf.String())
}

// EnforcePromptInput runs the full obfuscation-aware detection pipeline.
// Thresholds match Rust: score >= 0.70 -> Block, score >= 0.55 -> Review.
func EnforcePromptInput(input string) PromptEnforcementDecision {
	chars := len(input)
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(input)))

	if chars == 0 {
		return PromptEnforcementDecision{
			Verdict: VerdictAllow, Score: 0, Action: ActionAllow,
			PromptHash: hash, PromptChars: chars,
		}
	}

	np := normalizePrompt(input)
	var reasons []InjectionReason
	var score float64

	for _, rule := range enhancedRules {
		matched := false
		for _, variant := range []string{np.lowered, np.collapsed, np.compact} {
			if rule.pattern.MatchString(variant) {
				matched = true
				break
			}
		}
		if matched {
			reasons = append(reasons, InjectionReason{
				Code: rule.code, Message: rule.message, Score: rule.score,
			})
			score += rule.score
		}
	}

	if np.hasInstructionOverride {
		score += 0.56
		reasons = append(reasons, InjectionReason{
			Code: "heuristic.instruction_override", Message: "instruction override detected via heuristic", Score: 0.56,
		})
	}
	if np.hasExfiltrationIntent {
		score += 0.24
		reasons = append(reasons, InjectionReason{
			Code: "heuristic.exfiltration_intent", Message: "exfiltration intent detected via heuristic", Score: 0.24,
		})
	}
	if np.hadZWSP {
		score += 0.08
		reasons = append(reasons, InjectionReason{
			Code: "obfuscation.zwsp", Message: "zero-width space characters detected", Score: 0.08,
		})
	}
	if np.hasBase64Marker {
		score += 0.08
		reasons = append(reasons, InjectionReason{
			Code: "obfuscation.base64", Message: "encoding obfuscation marker detected", Score: 0.08,
		})
	}

	if score > 1.0 {
		score = 1.0
	}

	var verdict PromptEnforcementVerdict
	var action PromptEnforcementAction
	switch {
	case score >= 0.70:
		verdict = VerdictBlock
		action = ActionBlocked
	case score >= 0.55:
		verdict = VerdictReview
		action = ActionReviewBlocked
	default:
		verdict = VerdictAllow
		action = ActionAllow
	}

	return PromptEnforcementDecision{
		Verdict: verdict, Score: score, Reasons: reasons, Action: action,
		PromptHash: hash, PromptChars: chars,
	}
}

// ── Tool definition scanning ──────────────────────────────────────────

// ScanToolDefinition checks a remote tool's name and description for
// prompt injection patterns. MCP tools and other externally-sourced
// tools may carry malicious descriptions designed to override system
// instructions. Returns a score and the list of matched patterns.
func ScanToolDefinition(name, description string) (score float64, reasons []string) {
	// Check the tool name — short names get a pass, but suspicious
	// patterns (e.g. names containing system-override keywords) are caught.
	if len(name) > 60 {
		score += 0.1
		reasons = append(reasons, "unusually_long_tool_name")
	}

	// Check the description for injection patterns using the same
	// detector pipeline used for user messages.
	combined := name + "\n" + description
	if combined != "" {
		descScore, descReasons := detectInjection(combined)
		if descScore > 0.3 {
			score += descScore * 0.5 // tool definitions are lower risk than user input
			reasons = append(reasons, descReasons...)
		}
	}

	// Specific patterns that indicate tool-description-based attacks.
	toolSpecificPatterns := []struct {
		pattern string
		reason  string
		weight  float64
	}{
		{"system:", "tool_def_contains_system_prefix", 0.4},
		{"<system>", "tool_def_contains_system_tag", 0.5},
		{"override", "tool_def_contains_override", 0.3},
		{"ignore previous", "tool_def_contains_ignore_previous", 0.5},
		{"you are now", "tool_def_contains_role_change", 0.5},
		{"new instructions", "tool_def_contains_new_instructions", 0.4},
		{"forget everything", "tool_def_contains_forget", 0.6},
	}

	lowerCombined := strings.ToLower(combined)
	for _, sp := range toolSpecificPatterns {
		if strings.Contains(lowerCombined, sp.pattern) {
			score += sp.weight
			reasons = append(reasons, sp.reason)
		}
	}

	if score > 1.0 {
		score = 1.0
	}
	return score, reasons
}

// detectInjection is the internal scoring helper used by both
// DetectPromptInjection and ScanToolDefinition.
func detectInjection(input string) (score float64, reasons []string) {
	lower := strings.ToLower(input)

	// Check system override patterns.
	for _, p := range systemOverridePatterns {
		if p.MatchString(lower) {
			score += 0.3
			reasons = append(reasons, "system_override")
			break
		}
	}

	// Check jailbreak patterns.
	for _, p := range jailbreakPatterns {
		if p.MatchString(lower) {
			score += 0.5
			reasons = append(reasons, "jailbreak")
			break
		}
	}

	// Check base64-encoded content (potential hidden payload).
	if base64Pattern.MatchString(input) {
		score += 0.2
		reasons = append(reasons, "base64_content")
	}

	if score > 1.0 {
		score = 1.0
	}
	return score, reasons
}

var base64Pattern = regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`)
