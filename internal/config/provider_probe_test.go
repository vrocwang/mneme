package config

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestProbeProviderReportsRealStatus guards the fix for a connection test that
// reported success whenever an API key string existed, without contacting
// anything: a wrong key, a typo'd base_url, or an unreachable host was all
// reported as healthy.
func TestProbeProviderReportsRealStatus(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/chat/completions" {
			t.Errorf("probe hit %q, want /chat/completions", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	status, err := probeProvider(ProviderConfig{
		Type: "openai", APIKey: "sk-test", BaseURL: srv.URL, Models: []string{"m"},
	}, "m")
	if err != nil {
		t.Fatalf("probe returned error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("Authorization = %q, want the provider key", gotAuth)
	}
}

func TestProbeProviderPropagatesAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	status, err := probeProvider(ProviderConfig{Type: "openai", APIKey: "bad", BaseURL: srv.URL}, "m")
	if err != nil {
		t.Fatalf("probe returned error: %v", err)
	}
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", status)
	}
}

func TestProbeProviderUnreachableEndpointFails(t *testing.T) {
	// Port 1 is not listening; the probe must surface a transport error rather
	// than an inferred success.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	_, err := probeProvider(ProviderConfig{Type: "openai", APIKey: "k", BaseURL: url}, "m")
	if err == nil {
		t.Fatal("expected a transport error for a closed endpoint, got nil")
	}
	if !strings.Contains(err.Error(), "request failed") {
		t.Fatalf("error = %v, want a request-failure description", err)
	}
}

func TestProbeProviderUsesAnthropicHeaders(t *testing.T) {
	var apiKey, version string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey = r.Header.Get("x-api-key")
		version = r.Header.Get("anthropic-version")
		if r.URL.Path != "/v1/messages" {
			t.Errorf("probe hit %q, want /v1/messages", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if _, err := probeProvider(ProviderConfig{Type: "anthropic", APIKey: "sk-ant", BaseURL: srv.URL}, "claude"); err != nil {
		t.Fatalf("probe returned error: %v", err)
	}
	if apiKey != "sk-ant" || version == "" {
		t.Fatalf("anthropic headers missing: api-key=%q version=%q", apiKey, version)
	}
}

func TestProbeProviderOllamaListsTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("probe hit %q, want /api/tags", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if _, err := probeProvider(ProviderConfig{Type: "ollama", BaseURL: srv.URL}, ""); err != nil {
		t.Fatalf("probe returned error: %v", err)
	}
}
