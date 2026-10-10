package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// providerProbeTimeout bounds a connection test. The test is user-triggered
// from the settings UI, so a bounded wait beats an indefinite spinner.
const providerProbeTimeout = 10 * time.Second

// probeProvider issues one real, minimal request to the provider's endpoint and
// returns the HTTP status. A non-nil error means the request could not be
// completed (DNS, TLS, connection refused, timeout). The caller decides whether
// the status means success.
//
// The request is deliberately the smallest thing that authenticates: a
// one-token chat completion for chat APIs and a tag listing for Ollama. Testing
// "connection" by looking only at whether an API key string exists (the
// previous behaviour) reports success for a wrong key or an unreachable host.
func probeProvider(p ProviderConfig, model string) (int, error) {
	client := &http.Client{Timeout: providerProbeTimeout}
	base := strings.TrimRight(p.BaseURL, "/")

	var (
		req *http.Request
		err error
	)
	switch p.Type {
	case "ollama":
		if base == "" {
			base = "http://localhost:11434"
		}
		req, err = http.NewRequest(http.MethodGet, base+"/api/tags", nil)
	default:
		body, _ := json.Marshal(map[string]interface{}{
			"model":      model,
			"max_tokens": 1,
			"messages":   []map[string]string{{"role": "user", "content": "ping"}},
		})
		if p.Type == "anthropic" {
			if base == "" {
				base = "https://api.anthropic.com"
			}
			req, err = http.NewRequest(http.MethodPost, base+"/v1/messages", bytes.NewReader(body))
			if err == nil {
				req.Header.Set("x-api-key", p.APIKey)
				req.Header.Set("anthropic-version", "2023-06-01")
			}
		} else {
			if base == "" {
				base = "https://api.openai.com/v1"
			}
			req, err = http.NewRequest(http.MethodPost, base+"/chat/completions", bytes.NewReader(body))
			if err == nil {
				req.Header.Set("Authorization", "Bearer "+p.APIKey)
			}
		}
	}
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}
