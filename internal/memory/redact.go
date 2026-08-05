package memory

import (
	"regexp"
	"strings"
)

// Redactor applies privacy-preserving transformations to text before storage.
type Redactor struct {
	patterns []redactionRule
}

type redactionRule struct {
	re   *regexp.Regexp
	repl string
	desc string
}

// NewRedactor creates a redactor with built-in PII detection patterns.
func NewRedactor() *Redactor {
	r := &Redactor{}
	r.patterns = []redactionRule{
		// Email addresses
		{re: regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`), repl: "[EMAIL]", desc: "email"},
		// Credit card numbers (basic pattern)
		{re: regexp.MustCompile(`\b(?:\d[ -]*?){13,16}\b`), repl: "[CARD]", desc: "credit_card"},
		// SSN (US)
		{re: regexp.MustCompile(`\b\d{3}[ -]\d{2}[ -]\d{4}\b`), repl: "[SSN]", desc: "ssn"},
		// API keys (common patterns)
		{re: regexp.MustCompile(`\b(sk-[a-zA-Z0-9]{20,})\b`), repl: "[API_KEY]", desc: "openai_key"},
		{re: regexp.MustCompile(`\b(xox[bprs]-[a-zA-Z0-9\-]+)\b`), repl: "[SLACK_TOKEN]", desc: "slack_token"},
		{re: regexp.MustCompile(`\b(gh[pousr]_[a-zA-Z0-9]{36,})\b`), repl: "[GITHUB_TOKEN]", desc: "github_token"},
		// Phone numbers (US/international)
		{re: regexp.MustCompile(`\b(\+?\d{1,2}[ -]?)?\(?\d{3}\)?[ -]?\d{3}[ -]?\d{4}\b`), repl: "[PHONE]", desc: "phone"},
		// IP addresses
		{re: regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`), repl: "[IP]", desc: "ip"},
		// JWT tokens
		{re: regexp.MustCompile(`\beyJ[a-zA-Z0-9\-_]{10,}\.[a-zA-Z0-9\-_]{10,}\.[a-zA-Z0-9\-_]{10,}\b`), repl: "[JWT]", desc: "jwt"},
		// AWS Access Key IDs
		{re: regexp.MustCompile(`\b(AKIA[0-9A-Z]{16})\b`), repl: "[AWS_KEY]", desc: "aws_key"},
		// Database connection strings
		{re: regexp.MustCompile(`\b(postgres|mysql|mongodb|redis)://[^ \n"']+`), repl: "[DB_URL]", desc: "db_url"},
		// Private key blocks
		{re: regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[^-]*-----END [A-Z ]*PRIVATE KEY-----`), repl: "[PRIVATE_KEY]", desc: "private_key"},
		// OAuth tokens
		{re: regexp.MustCompile(`\b(ya29\.[0-9A-Za-z\-_]+)\b`), repl: "[OAUTH_TOKEN]", desc: "oauth_token"},
		// Discord webhook URLs
		{re: regexp.MustCompile(`https://discord\.com/api/webhooks/\d+/[a-zA-Z0-9\-_]+`), repl: "[DISCORD_WEBHOOK]", desc: "discord_webhook"},
		// Bitcoin addresses
		{re: regexp.MustCompile(`\b(bc1[ac-hj-np-z02-9]{39,59}|[13][a-km-zA-HJ-NP-Z1-9]{25,34})\b`), repl: "[BTC_ADDR]", desc: "bitcoin_address"},
	}

	return r
}

// Redact applies all redaction rules and returns the sanitized text and a list of what was found.
func (r *Redactor) Redact(text string) (string, []string) {
	var found []string
	result := text
	for _, rule := range r.patterns {
		if rule.re.MatchString(result) {
			found = append(found, rule.desc)
			result = rule.re.ReplaceAllString(result, rule.repl)
		}
	}
	return result, found
}

var unsafeFilenameChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

// SanitizeFilename removes characters that are unsafe in file paths.
func SanitizeFilename(name string) string {
	name = unsafeFilenameChars.ReplaceAllString(name, "_")
	name = strings.TrimSpace(name)
	if len(name) > 200 {
		name = name[:200]
	}
	if name == "" {
		name = "unnamed"
	}
	return name
}
