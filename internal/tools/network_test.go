package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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

// TestHTTPGet_ProxyRejectsPrivateTargetBeforeRequest is the proxy SSRF
// regression: with a proxy configured, DialContext only sees the proxy, so the
// target host must be resolved and rejected before the request is handed to the
// proxy. The fake guard rejects only the target hostname, so the proxy's own
// loopback address cannot be what fails the request: without the target-side
// check the request would reach the proxy.
func TestHTTPGet_ProxyRejectsPrivateTargetBeforeRequest(t *testing.T) {
	origValidate := validateURLFn
	origGuard := ssrfDialGuardFn
	validateURLFn = func(string) error { return nil }

	var mu sync.Mutex
	var guarded []string
	ssrfDialGuardFn = func(_ context.Context, host string) error {
		mu.Lock()
		guarded = append(guarded, host)
		mu.Unlock()
		if host == "rebind.internal" {
			return fmt.Errorf("dial: %q resolves to private/internal address 10.0.0.1", host)
		}
		return nil
	}
	defer func() {
		validateURLFn = origValidate
		ssrfDialGuardFn = origGuard
	}()

	var proxyHits int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&proxyHits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer proxy.Close()

	tool := NewHTTPGet(config.ProxyConfig{HTTPProxy: proxy.URL, NoProxy: "example.invalid"})
	res := tool.Execute(context.Background(), map[string]interface{}{"url": "http://rebind.internal/data"})
	if res.Success {
		t.Fatalf("expected the private target through a proxy to be rejected, got %+v", res)
	}
	if !strings.Contains(res.Error, "rebind.internal") {
		t.Errorf("error %q does not name the rejected target", res.Error)
	}
	if n := atomic.LoadInt32(&proxyHits); n != 0 {
		t.Errorf("proxy received %d requests, want 0 (target must be validated before sending)", n)
	}

	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, host := range guarded {
		if host == "rebind.internal" {
			found = true
		}
	}
	if !found {
		t.Errorf("dial guard was never asked about the target host; guarded=%v", guarded)
	}
}

// headerRecorder captures a header value from a server handler without racing
// the test goroutine.
type headerRecorder struct {
	mu     sync.Mutex
	header string
}

func (r *headerRecorder) set(v string) {
	r.mu.Lock()
	r.header = v
	r.mu.Unlock()
}

func (r *headerRecorder) get() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.header
}

// TestHTTPGet_RedirectDropsCallerHeadersCrossHost is the redirect header-leak
// regression: net/http re-sends caller-supplied headers on a redirect and only
// strips Authorization/Cookie, so a custom credential header must be dropped
// when the redirect changes host while a same-host redirect keeps it.
func TestHTTPGet_RedirectDropsCallerHeadersCrossHost(t *testing.T) {
	origValidate := validateURLFn
	origGuard := ssrfDialGuardFn
	validateURLFn = func(string) error { return nil }
	ssrfDialGuardFn = func(context.Context, string) error { return nil }
	defer func() {
		validateURLFn = origValidate
		ssrfDialGuardFn = origGuard
	}()

	var same, cross headerRecorder
	var sameSrv *httptest.Server
	sameSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/end" {
			same.set(r.Header.Get("X-Api-Key"))
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, sameSrv.URL+"/end", http.StatusFound)
	}))
	defer sameSrv.Close()

	// A second server simulates a different host (same loopback IP, other port).
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cross.set(r.Header.Get("X-Api-Key"))
		w.WriteHeader(http.StatusOK)
	}))
	defer other.Close()

	crossSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/end", http.StatusFound)
	}))
	defer crossSrv.Close()

	tool := NewHTTPGet(config.ProxyConfig{})
	args := func(url string) map[string]interface{} {
		return map[string]interface{}{
			"url":     url,
			"headers": map[string]interface{}{"X-Api-Key": "secret"},
		}
	}

	res := tool.Execute(context.Background(), args(sameSrv.URL+"/start"))
	if !res.Success {
		t.Fatalf("same-host redirect request failed: %s", res.Error)
	}
	if got := same.get(); got != "secret" {
		t.Errorf("same-host redirect header = %q, want the caller header preserved", got)
	}

	res = tool.Execute(context.Background(), args(crossSrv.URL+"/start"))
	if !res.Success {
		t.Fatalf("cross-host redirect request failed: %s", res.Error)
	}
	if got := cross.get(); got != "" {
		t.Errorf("cross-host redirect leaked X-Api-Key = %q to the redirect target", got)
	}
}
