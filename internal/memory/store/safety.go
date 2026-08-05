package store

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// SanitizationReport summarizes what was found and redacted during a
// safety pass over a piece of content.
type SanitizationReport struct {
	SecretsFound   int      `json:"secrets_found"`
	PIIFound       int      `json:"pii_found"`
	SecretPatterns []string `json:"secret_patterns,omitempty"`
	PIIPatterns    []string `json:"pii_patterns,omitempty"`
	BytesOriginal  int      `json:"bytes_original"`
	BytesSanitized int      `json:"bytes_sanitized"`
}

// ── Secret/credential patterns ──────────────────────────────────────────

var secretPatterns = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{"aws_access_key", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{"aws_secret_key", regexp.MustCompile(`(?i)aws(.{0,20})?(secret|key).{0,10}[:=]\s*['\"]?([A-Za-z0-9/+=]{40})`)},
	{"github_token", regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]{36,}`)},
	{"github_pat", regexp.MustCompile(`github_pat_[A-Za-z0-9_]{36,}`)},
	{"jwt_token", regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)},
	{"slack_token", regexp.MustCompile(`xox[baprs]-[0-9A-Za-z-]{10,}`)},
	{"google_api_key", regexp.MustCompile(`AIza[0-9A-Za-z\-_]{35}`)},
	{"stripe_sk", regexp.MustCompile(`sk_live_[0-9a-zA-Z]{24,}`)},
	{"stripe_pk", regexp.MustCompile(`pk_live_[0-9a-zA-Z]{24,}`)},
	{"openai_key", regexp.MustCompile(`sk-[A-Za-z0-9]{32,}`)},
	{"anthropic_key", regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{32,}`)},
	{"generic_api_key", regexp.MustCompile(`(?i)(api[_-]?key|apikey|api_secret)\s*[:=]\s*['\"]?([A-Za-z0-9+/=_\-]{20,})`)},
	{"private_key_pem", regexp.MustCompile(`-----BEGIN (RSA |EC |DSA |OPENSSH |ENCRYPTED )?PRIVATE KEY-----`)},
	{"ssh_private_key", regexp.MustCompile(`-----BEGIN OPENSSH PRIVATE KEY-----`)},
	{"pgp_private_key", regexp.MustCompile(`-----BEGIN PGP PRIVATE KEY BLOCK-----`)},
	{"discord_token", regexp.MustCompile(`[MN][A-Za-z\d]{23}\.[\w-]{6}\.[\w-]{27}`)},
	{"heroku_key", regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)},
	{"basic_auth_header", regexp.MustCompile(`(?i)authorization\s*[:=]\s*(basic|bearer)\s+[A-Za-z0-9+/=_-]+`)},
	{"connection_string", regexp.MustCompile(`(?i)(mongodb|mysql|postgres|postgresql|redis|sqlite)://[^\s'"]+`)},
	{"password_in_json", regexp.MustCompile(`(?i)"password"\s*:\s*"[^"]+"`)},
	{"token_in_json", regexp.MustCompile(`(?i)"token"\s*:\s*"[^"]+"`)},
	{"secret_in_json", regexp.MustCompile(`(?i)"secret"\s*:\s*"[^"]+"`)},
}

// ── PII patterns ────────────────────────────────────────────────────────

var piiPatterns = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{"email", regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)},
	{"credit_card", regexp.MustCompile(`\b(?:4[0-9]{12}(?:[0-9]{3})?|5[1-5][0-9]{14}|3[47][0-9]{13}|6(?:011|5[0-9]{2})[0-9]{12})\b`)},
	{"ssn_us", regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)},
	{"phone_us", regexp.MustCompile(`\b\+?1?[ -]?\(?\d{3}\)?[ -]?\d{3}[ -]?\d{4}\b`)},
	{"ip_address", regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\b`)},
	{"cpf_br", regexp.MustCompile(`\b\d{3}\.\d{3}\.\d{3}-\d{2}\b`)},            // Brazil
	{"cnpj_br", regexp.MustCompile(`\b\d{2}\.\d{3}\.\d{3}/\d{4}-\d{2}\b`)},     // Brazil company
	{"rfc_mx", regexp.MustCompile(`\b[A-ZÑ&]{3,4}\d{6}[A-Z0-9]{3}\b`)},         // Mexico
	{"curp_mx", regexp.MustCompile(`\b[A-Z]{4}\d{6}[HM][A-Z]{5}[A-Z0-9]\d\b`)}, // Mexico
	{"dni_ar", regexp.MustCompile(`\b\d{2}\.?\d{3}\.?\d{3}\b`)},                // Argentina
	{"iban", regexp.MustCompile(`\b[A-Z]{2}\d{2}[A-Z0-9]{4,30}\b`)},
}

// SanitizeContent scans content for secrets and PII, replacing matches
// with redaction markers. Returns the sanitized content and a report.
func SanitizeContent(content string) (string, SanitizationReport) {
	report := SanitizationReport{
		BytesOriginal: len(content),
	}

	sanitized := content

	// Scan for secrets (higher priority — replace first).
	for _, sp := range secretPatterns {
		matches := sp.pattern.FindAllString(sanitized, -1)
		if len(matches) > 0 {
			report.SecretsFound += len(matches)
			report.SecretPatterns = append(report.SecretPatterns, sp.name)
			for _, m := range matches {
				sanitized = strings.ReplaceAll(sanitized, m,
					fmt.Sprintf("<redacted:%s>", sp.name))
			}
		}
	}

	// Scan for PII.
	for _, pp := range piiPatterns {
		matches := pp.pattern.FindAllString(sanitized, -1)
		if len(matches) > 0 {
			report.PIIFound += len(matches)
			report.PIIPatterns = append(report.PIIPatterns, pp.name)
			for _, m := range matches {
				sanitized = strings.ReplaceAll(sanitized, m,
					fmt.Sprintf("<redacted:%s>", pp.name))
			}
		}
	}

	report.BytesSanitized = len(sanitized)
	return sanitized, report
}

// SanitizeChunk sanitizes the content and summary of a memory chunk,
// returning a sanitized copy and a report.
func SanitizeChunk(chunk MemoryChunk) (MemoryChunk, SanitizationReport) {
	sanitizedContent, contentReport := SanitizeContent(chunk.Content)
	sanitizedSummary, summaryReport := SanitizeContent(chunk.Summary)

	report := SanitizationReport{
		SecretsFound:   contentReport.SecretsFound + summaryReport.SecretsFound,
		PIIFound:       contentReport.PIIFound + summaryReport.PIIFound,
		SecretPatterns: dedupStrings(append(contentReport.SecretPatterns, summaryReport.SecretPatterns...)),
		PIIPatterns:    dedupStrings(append(contentReport.PIIPatterns, summaryReport.PIIPatterns...)),
		BytesOriginal:  contentReport.BytesOriginal + summaryReport.BytesOriginal,
		BytesSanitized: contentReport.BytesSanitized + summaryReport.BytesSanitized,
	}

	chunk.Content = sanitizedContent
	chunk.Summary = sanitizedSummary
	return chunk, report
}

// SanitizeJSON walks a JSON value tree and redacts any string values
// that contain secrets or PII. Returns the sanitized JSON string.
func SanitizeJSON(rawJSON string) (string, SanitizationReport, error) {
	var v interface{}
	if err := json.Unmarshal([]byte(rawJSON), &v); err != nil {
		return rawJSON, SanitizationReport{}, err
	}

	sanitized, report := sanitizeJSONValue(v)
	out, err := json.Marshal(sanitized)
	if err != nil {
		return rawJSON, report, err
	}
	return string(out), report, nil
}

func sanitizeJSONValue(v interface{}) (interface{}, SanitizationReport) {
	report := SanitizationReport{}
	switch val := v.(type) {
	case string:
		s, r := SanitizeContent(val)
		report.merge(&r)
		return s, report
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, vv := range val {
			san, r := sanitizeJSONValue(vv)
			report.merge(&r)
			out[k] = san
		}
		return out, report
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, vv := range val {
			san, r := sanitizeJSONValue(vv)
			report.merge(&r)
			out[i] = san
		}
		return out, report
	default:
		return v, report
	}
}

func (r *SanitizationReport) merge(other *SanitizationReport) {
	if other == nil {
		return
	}
	r.SecretsFound += other.SecretsFound
	r.PIIFound += other.PIIFound
	r.SecretPatterns = dedupStrings(append(r.SecretPatterns, other.SecretPatterns...))
	r.PIIPatterns = dedupStrings(append(r.PIIPatterns, other.PIIPatterns...))
	r.BytesOriginal += other.BytesOriginal
	r.BytesSanitized += other.BytesSanitized
}

func dedupStrings(items []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, s := range items {
		if s == "" {
			continue
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
