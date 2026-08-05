// Package safety provides content safety scanning for memory ingestion.
// It detects PII, secrets, and policy-violating content before storage,
// drawing on the regex patterns already established in the memory/store and
// memory packages for consistency.
package safety

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ── Issue classification types ────────────────────────────────────────────

// IssueKind classifies the category of a safety issue.
type IssueKind string

const (
	KindPII      IssueKind = "pii"
	KindSecret   IssueKind = "secret"
	KindViolence IssueKind = "violence"
	KindHate     IssueKind = "hate"
	KindIllegal  IssueKind = "illegal"
	KindSpam     IssueKind = "spam"
)

// Severity indicates how serious an issue is.
type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// ── SafetyIssue ────────────────────────────────────────────────────────────

// Location pinpoints a span of text that triggered a rule.
type Location struct {
	Offset int `json:"offset"` // byte offset from start of content
	Length int `json:"length"` // byte length of the match
}

// SafetyIssue describes a single safety violation found in content.
type SafetyIssue struct {
	Kind     IssueKind `json:"kind"`
	Severity Severity  `json:"severity"`
	Message  string    `json:"message"`
	Location Location  `json:"location"`
}

// ── Checker ────────────────────────────────────────────────────────────────

// Checker runs a configurable set of safety rules over text content.
type Checker struct {
	rules []checkRule

	// BlockOnMultipleHigh determines the threshold for ShouldBlock
	// when more than this many high-severity issues are found. Default 2.
	BlockOnMultipleHigh int
}

// checkRule pairs a compiled pattern with its metadata.
type checkRule struct {
	name     string
	pattern  *regexp.Regexp
	kind     IssueKind
	severity Severity
	message  string
}

// NewChecker creates a Checker with all built-in safety rules.
func NewChecker() *Checker {
	return &Checker{
		rules:               builtinRules(),
		BlockOnMultipleHigh: 2,
	}
}

// NewCheckerWithRules creates a Checker that only uses the supplied rules.
// Use this when you want to augment or replace the built-in set.
func NewCheckerWithRules(rules []checkRule) *Checker {
	return &Checker{
		rules:               rules,
		BlockOnMultipleHigh: 2,
	}
}

// builtinRules returns the standard set of safety checks.
// Patterns are drawn from the redaction rules in memory/redact.go and
// memory/store/safety.go for consistency across the codebase.
func builtinRules() []checkRule {
	return []checkRule{
		// ── Secrets / credentials (critical severity) ──────────────────────
		{
			name:     "openai_key",
			pattern:  regexp.MustCompile(`\bsk-[A-Za-z0-9]{32,}\b`),
			kind:     KindSecret,
			severity: SeverityCritical,
			message:  "OpenAI API key detected",
		},
		{
			name:     "anthropic_key",
			pattern:  regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{32,}\b`),
			kind:     KindSecret,
			severity: SeverityCritical,
			message:  "Anthropic API key detected",
		},
		{
			name:     "private_key_pem",
			pattern:  regexp.MustCompile(`-----BEGIN (RSA |EC |DSA |OPENSSH |ENCRYPTED )?PRIVATE KEY-----`),
			kind:     KindSecret,
			severity: SeverityCritical,
			message:  "Private key in PEM format detected",
		},
		{
			name:     "generic_api_key",
			pattern:  regexp.MustCompile(`(?i)(api[_-]?key|apikey|api_secret)[\s]*[:=][\s]*['\x22]?([A-Za-z0-9+/=_\-]{20,})`),
			kind:     KindSecret,
			severity: SeverityCritical,
			message:  "Generic API key assignment detected",
		},
		{
			name:     "github_token",
			pattern:  regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{36,}\b`),
			kind:     KindSecret,
			severity: SeverityCritical,
			message:  "GitHub personal access token detected",
		},
		{
			name:     "github_pat",
			pattern:  regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{36,}\b`),
			kind:     KindSecret,
			severity: SeverityCritical,
			message:  "GitHub fine-grained PAT detected",
		},
		{
			name:     "slack_token",
			pattern:  regexp.MustCompile(`\bxox[baprs]-[0-9A-Za-z-]{10,}\b`),
			kind:     KindSecret,
			severity: SeverityCritical,
			message:  "Slack token detected",
		},
		{
			name:     "aws_access_key",
			pattern:  regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
			kind:     KindSecret,
			severity: SeverityCritical,
			message:  "AWS access key detected",
		},
		{
			name:     "aws_secret_key",
			pattern:  regexp.MustCompile(`(?i)aws(.{0,20})?(secret|key).{0,10}[:=]\s*['\x22]?([A-Za-z0-9/+=]{40})`),
			kind:     KindSecret,
			severity: SeverityCritical,
			message:  "AWS secret key assignment detected",
		},
		{
			name:     "google_api_key",
			pattern:  regexp.MustCompile(`\bAIza[0-9A-Za-z\-_]{35}\b`),
			kind:     KindSecret,
			severity: SeverityCritical,
			message:  "Google API key detected",
		},
		{
			name:     "stripe_sk",
			pattern:  regexp.MustCompile(`\bsk_live_[0-9a-zA-Z]{24,}\b`),
			kind:     KindSecret,
			severity: SeverityCritical,
			message:  "Stripe secret key detected",
		},
		{
			name:     "stripe_pk",
			pattern:  regexp.MustCompile(`\bpk_live_[0-9a-zA-Z]{24,}\b`),
			kind:     KindSecret,
			severity: SeverityMedium,
			message:  "Stripe publishable key detected",
		},
		{
			name:     "bearer_token",
			pattern:  regexp.MustCompile(`(?i)authorization\s*[:=]\s*bearer\s+[A-Za-z0-9+/=_.-]{20,}`),
			kind:     KindSecret,
			severity: SeverityCritical,
			message:  "Bearer authorization header detected",
		},
		{
			name:     "basic_auth",
			pattern:  regexp.MustCompile(`(?i)authorization\s*[:=]\s*basic\s+[A-Za-z0-9+/=]+`),
			kind:     KindSecret,
			severity: SeverityCritical,
			message:  "Basic authorization header detected",
		},
		{
			name:     "connection_string",
			pattern:  regexp.MustCompile(`(?i)(mongodb|mysql|postgres|postgresql|redis|sqlite)://[^\s'"]+`),
			kind:     KindSecret,
			severity: SeverityCritical,
			message:  "Database connection string detected",
		},
		{
			name:     "discord_token",
			pattern:  regexp.MustCompile(`\b[MN][A-Za-z\d]{23}\.[\w-]{6}\.[\w-]{27}\b`),
			kind:     KindSecret,
			severity: SeverityCritical,
			message:  "Discord token detected",
		},
		{
			name:     "heroku_key",
			pattern:  regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`),
			kind:     KindSecret,
			severity: SeverityMedium,
			message:  "Possible Heroku-style API key (UUID format)",
		},
		{
			name:     "ssh_private_key",
			pattern:  regexp.MustCompile(`-----BEGIN OPENSSH PRIVATE KEY-----`),
			kind:     KindSecret,
			severity: SeverityCritical,
			message:  "OpenSSH private key detected",
		},
		{
			name:     "pgp_private_key",
			pattern:  regexp.MustCompile(`-----BEGIN PGP PRIVATE KEY BLOCK-----`),
			kind:     KindSecret,
			severity: SeverityCritical,
			message:  "PGP private key block detected",
		},
		{
			name:     "password_in_json",
			pattern:  regexp.MustCompile(`(?i)"password"\s*:\s*"[^"]+"`),
			kind:     KindSecret,
			severity: SeverityHigh,
			message:  "Password field detected in JSON",
		},
		{
			name:     "token_in_json",
			pattern:  regexp.MustCompile(`(?i)"token"\s*:\s*"[^"]+"`),
			kind:     KindSecret,
			severity: SeverityHigh,
			message:  "Token field detected in JSON",
		},
		{
			name:     "secret_in_json",
			pattern:  regexp.MustCompile(`(?i)"secret"\s*:\s*"[^"]+"`),
			kind:     KindSecret,
			severity: SeverityHigh,
			message:  "Secret field detected in JSON",
		},

		// ── PII (high severity) ────────────────────────────────────────────
		{
			name:     "credit_card",
			pattern:  regexp.MustCompile(`\b(?:4[0-9]{12}(?:[0-9]{3})?|5[1-5][0-9]{14}|3[47][0-9]{13}|6(?:011|5[0-9]{2})[0-9]{12})\b`),
			kind:     KindPII,
			severity: SeverityHigh,
			message:  "Credit card number detected",
		},
		{
			name:     "ssn_us",
			pattern:  regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
			kind:     KindPII,
			severity: SeverityHigh,
			message:  "US Social Security Number detected",
		},
		{
			name:     "email",
			pattern:  regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`),
			kind:     KindPII,
			severity: SeverityMedium,
			message:  "Email address detected",
		},
		{
			name:     "phone_us",
			pattern:  regexp.MustCompile(`\b\+?1?[ -]?\(?\d{3}\)?[ -]?\d{3}[ -]?\d{4}\b`),
			kind:     KindPII,
			severity: SeverityMedium,
			message:  "US phone number detected",
		},
		{
			name:     "jwt_token",
			pattern:  regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`),
			kind:     KindSecret,
			severity: SeverityHigh,
			message:  "JWT token detected",
		},
		{
			name:     "passwords_in_text",
			pattern:  regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[:=]\s*\S+`),
			kind:     KindSecret,
			severity: SeverityHigh,
			message:  "Password assignment in plain text detected",
		},
	}
}

// ── Public API ─────────────────────────────────────────────────────────────

// CheckContent runs all configured rules against text and returns every issue
// found, ordered by byte offset.
func (c *Checker) CheckContent(text string) []SafetyIssue {
	var issues []SafetyIssue

	for _, rule := range c.rules {
		locs := rule.pattern.FindAllStringIndex(text, -1)
		for _, loc := range locs {
			issues = append(issues, SafetyIssue{
				Kind:     rule.kind,
				Severity: rule.severity,
				Message:  rule.message,
				Location: Location{
					Offset: loc[0],
					Length: loc[1] - loc[0],
				},
			})
		}
	}

	// Sort by byte offset as documented.
	sort.Slice(issues, func(i, j int) bool {
		return issues[i].Location.Offset < issues[j].Location.Offset
	})

	return issues
}

// ShouldBlock determines whether the set of issues is severe enough to
// warrant blocking ingestion. It blocks when:
//   - Any issue has critical severity, or
//   - Two or more issues have high severity (configurable via BlockOnMultipleHigh).
func (c *Checker) ShouldBlock(issues []SafetyIssue) bool {
	highCount := 0
	for _, issue := range issues {
		switch issue.Severity {
		case SeverityCritical:
			return true
		case SeverityHigh:
			highCount++
			if highCount >= c.BlockOnMultipleHigh {
				return true
			}
		}
	}
	return false
}

// FilterByKind returns only issues matching the given kinds.
func FilterByKind(issues []SafetyIssue, kinds ...IssueKind) []SafetyIssue {
	set := make(map[IssueKind]bool, len(kinds))
	for _, k := range kinds {
		set[k] = true
	}
	var out []SafetyIssue
	for _, issue := range issues {
		if set[issue.Kind] {
			out = append(out, issue)
		}
	}
	return out
}

// FilterBySeverity returns only issues matching the given severity.
func FilterBySeverity(issues []SafetyIssue, severity Severity) []SafetyIssue {
	var out []SafetyIssue
	for _, issue := range issues {
		if issue.Severity == severity {
			out = append(out, issue)
		}
	}
	return out
}

// IssueCounts returns a summary map mapping severity levels to counts.
func IssueCounts(issues []SafetyIssue) map[Severity]int {
	counts := make(map[Severity]int)
	for _, issue := range issues {
		counts[issue.Severity]++
	}
	return counts
}

// WorstSeverity returns the highest severity among the given issues.
// Returns empty string if there are no issues.
func WorstSeverity(issues []SafetyIssue) Severity {
	worst := Severity("")
	for _, issue := range issues {
		if severityRank(issue.Severity) > severityRank(worst) {
			worst = issue.Severity
		}
	}
	return worst
}

func severityRank(s Severity) int {
	switch s {
	case SeverityLow:
		return 1
	case SeverityMedium:
		return 2
	case SeverityHigh:
		return 3
	case SeverityCritical:
		return 4
	default:
		return 0
	}
}

// Report returns a human-readable summary of the issues found.
func Report(issues []SafetyIssue) string {
	if len(issues) == 0 {
		return "no safety issues found"
	}

	counts := IssueCounts(issues)
	var parts []string
	for _, sev := range []Severity{SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow} {
		if n, ok := counts[sev]; ok {
			parts = append(parts, fmt.Sprintf("%d %s", n, sev))
		}
	}
	return strings.Join(parts, ", ")
}

// HasIssueKind returns true if any issue matches the given kind.
func HasIssueKind(issues []SafetyIssue, kind IssueKind) bool {
	for _, issue := range issues {
		if issue.Kind == kind {
			return true
		}
	}
	return false
}
