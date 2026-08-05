package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/simon/mneme/internal/config"
)

func TestHTTPGet_Success(t *testing.T) {
	// Override URL validation and dial guard for local test servers.
	origValidate := validateURLFn
	origDialGuard := ssrfDialGuardFn
	validateURLFn = func(rawURL string) error { return nil }
	ssrfDialGuardFn = func(ctx context.Context, host string) error { return nil }
	defer func() {
		validateURLFn = origValidate
		ssrfDialGuardFn = origDialGuard
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello from server"))
	}))
	defer server.Close()

	tool := NewHTTPGet(config.ProxyConfig{})
	result := tool.Execute(context.Background(), map[string]interface{}{
		"url": server.URL,
	})

	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if !containsText(result.Output, "hello from server") {
		t.Errorf("expected server response in output: %s", result.Output)
	}
}

func TestHTTPGet_MissingURL(t *testing.T) {
	tool := NewHTTPGet(config.ProxyConfig{})
	result := tool.Execute(context.Background(), map[string]interface{}{})
	if result.Success {
		t.Error("expected failure without url")
	}
}

func TestHTTPPost_Success(t *testing.T) {
	origValidate := validateURLFn
	origDialGuard := ssrfDialGuardFn
	validateURLFn = func(rawURL string) error { return nil }
	ssrfDialGuardFn = func(ctx context.Context, host string) error { return nil }
	defer func() {
		validateURLFn = origValidate
		ssrfDialGuardFn = origDialGuard
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("created"))
	}))
	defer server.Close()

	tool := NewHTTPPost(config.ProxyConfig{})
	result := tool.Execute(context.Background(), map[string]interface{}{
		"url":  server.URL,
		"body": `{"key":"value"}`,
	})

	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
}

func containsText(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
