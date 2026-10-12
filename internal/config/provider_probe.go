package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
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
// The optional proxy carries the [proxy] settings from config.toml so the probe
// honours a configured proxy. A bare http.Client only honours the process
// environment, so behind a proxy configured solely in config.toml the probe
// used to report a false failure. With no proxy argument (or an empty one), the
// previous behaviour is kept exactly: the default transport is used.
//
// The request is deliberately the smallest thing that authenticates: a
// one-token chat completion for chat APIs and a tag listing for Ollama. Testing
// "connection" by looking only at whether an API key string exists (the
// previous behaviour) reports success for a wrong key or an unreachable host.
func probeProvider(p ProviderConfig, model string, proxy ...ProxyConfig) (int, error) {
	client := &http.Client{Timeout: providerProbeTimeout}
	if len(proxy) > 0 && (proxy[0].HTTPProxy != "" || proxy[0].HTTPSProxy != "") {
		client.Transport = &http.Transport{Proxy: probeProxyFunc(proxy[0])}
	}
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

// probeProxyFunc builds the proxy selector for a probe from the config's
// [proxy] section, mirroring the wiring the agent's network tools use: an
// explicit http_proxy/https_proxy/no_proxy wins, otherwise the process
// environment applies. It is replicated here (rather than shared) because the
// tools helper is unexported and importing internal/tools from config would
// cycle.
func probeProxyFunc(cfg ProxyConfig) func(*http.Request) (*url.URL, error) {
	if cfg.HTTPProxy == "" && cfg.HTTPSProxy == "" {
		return http.ProxyFromEnvironment
	}

	httpProxyURL, err := url.Parse(cfg.HTTPProxy)
	if err != nil && cfg.HTTPProxy != "" {
		return http.ProxyFromEnvironment
	}
	httpsProxyURL, err := url.Parse(cfg.HTTPSProxy)
	if err != nil && cfg.HTTPSProxy != "" {
		return http.ProxyFromEnvironment
	}

	noProxy := make(map[string]bool)
	addNoProxy := func(list string) {
		for _, host := range strings.Split(list, ",") {
			if host = strings.TrimSpace(host); host != "" {
				noProxy[host] = true
			}
		}
	}
	addNoProxy(cfg.NoProxy)
	if cfg.NoProxy == "" {
		// Only consult the environment when config did not set no_proxy, so an
		// explicit config value is authoritative.
		addNoProxy(os.Getenv("NO_PROXY"))
	}

	return func(req *http.Request) (*url.URL, error) {
		if noProxy[req.URL.Hostname()] {
			return nil, nil
		}
		if req.URL.Scheme == "https" && httpsProxyURL != nil {
			return httpsProxyURL, nil
		}
		if httpProxyURL != nil {
			return httpProxyURL, nil
		}
		return http.ProxyFromEnvironment(req)
	}
}
