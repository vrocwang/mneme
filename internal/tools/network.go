package tools

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/simon/mneme/internal/config"
)

// validateURLFn is the URL validation function, overridable in tests.
var validateURLFn = validateURL

// ssrfDialGuardFn validates resolved IPs at connection time. Overridable in tests.
var ssrfDialGuardFn = blockPrivateIPs

// HTTPGet performs an HTTP GET request.
type HTTPGet struct {
	client      *http.Client
	proxyConfig config.ProxyConfig
}

func NewHTTPGet(proxyConfig config.ProxyConfig) *HTTPGet {
	return &HTTPGet{
		client:      buildHTTPClient(proxyConfig),
		proxyConfig: proxyConfig,
	}
}

func (t *HTTPGet) Schema() Schema {
	return Schema{
		Name:        "http_get",
		Description: "Perform an HTTP GET request",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "URL to fetch",
				},
			},
			"required": []string{"url"},
		},
	}
}

func (t *HTTPGet) PermissionLevel() PermissionLevel { return PermExecute }
func (t *HTTPGet) SideEffects() bool                { return true }

func (t *HTTPGet) Execute(ctx context.Context, args map[string]interface{}) Result {
	rawURL, _ := args["url"].(string)
	if rawURL == "" {
		return Result{Error: "url is required"}
	}

	if err := validateURLFn(rawURL); err != nil {
		return Result{Error: fmt.Sprintf("url rejected: %v", err)}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return Result{Error: fmt.Sprintf("create request: %v", err)}
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return Result{Error: fmt.Sprintf("http get: %v", err)}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return Result{Error: fmt.Sprintf("read body: %v", err)}
	}

	return Result{
		Success: true,
		Output:  fmt.Sprintf("Status: %d\n%s", resp.StatusCode, string(body)),
	}
}

// HTTPPost performs an HTTP POST request.
type HTTPPost struct {
	client      *http.Client
	proxyConfig config.ProxyConfig
}

func NewHTTPPost(proxyConfig config.ProxyConfig) *HTTPPost {
	return &HTTPPost{
		client:      buildHTTPClient(proxyConfig),
		proxyConfig: proxyConfig,
	}
}

func (t *HTTPPost) Schema() Schema {
	return Schema{
		Name:        "http_post",
		Description: "Perform an HTTP POST request with a JSON body",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "URL to post to",
				},
				"body": map[string]interface{}{
					"type":        "string",
					"description": "JSON body string",
				},
			},
			"required": []string{"url", "body"},
		},
	}
}

func (t *HTTPPost) PermissionLevel() PermissionLevel { return PermExecute }
func (t *HTTPPost) SideEffects() bool                { return true }

func (t *HTTPPost) Execute(ctx context.Context, args map[string]interface{}) Result {
	rawURL, _ := args["url"].(string)
	bodyStr, _ := args["body"].(string)
	if rawURL == "" {
		return Result{Error: "url is required"}
	}

	if err := validateURLFn(rawURL); err != nil {
		return Result{Error: fmt.Sprintf("url rejected: %v", err)}
	}

	req, err := http.NewRequestWithContext(ctx, "POST", rawURL, strings.NewReader(bodyStr))
	if err != nil {
		return Result{Error: fmt.Sprintf("create request: %v", err)}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return Result{Error: fmt.Sprintf("http post: %v", err)}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{Error: fmt.Sprintf("read body: %v", err)}
	}
	return Result{
		Success: true,
		Output:  fmt.Sprintf("Status: %d\n%s", resp.StatusCode, string(body)),
	}
}

// validateURL checks a URL for SSRF risks: only allows http/https schemes and
// rejects requests to private/internal IP addresses.
func validateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q is not allowed (only http/https)", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL has no host")
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("cannot resolve host %q: %w", host, err)
	}

	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("URL resolves to private/internal address %s", ip)
		}
	}

	return nil
}

// isPrivateIP returns true if the IP is a private, loopback, link-local,
// multicast, or otherwise internal address that should not be reachable.
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	// Normalize IPv4-mapped IPv6 addresses (::ffff:a.b.c.d) to their IPv4 form
	// so they are classified by the IPv4 rules below. IsLoopback/IsPrivate
	// return false for the mapped form otherwise.
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}

	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if ip.IsPrivate() {
		return true
	}
	if ip.Equal(net.IPv4(169, 254, 169, 254)) {
		return true
	}

	// Cover ranges that IsPrivate() does not: CGNAT, benchmark, multicast and
	// reserved ranges. Using explicit *net.IPNet members is unambiguous.
	for _, block := range ssrfBlockedNetworks {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// ssrfBlockedNetworks holds CIDR ranges that must never be reachable by agent
// network tools but are not covered by net.IP.IsPrivate.
var ssrfBlockedNetworks = mustParseCIDRs(
	"0.0.0.0/8",       // "this network"
	"100.64.0.0/10",   // carrier-grade NAT
	"169.254.0.0/16",  // link-local (also covered by IsLinkLocalUnicast)
	"198.18.0.0/15",   // benchmarking
	"224.0.0.0/4",     // multicast
	"240.0.0.0/4",     // reserved (including 255.255.255.255/32)
)

func mustParseCIDRs(cidrs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

// blockPrivateIPs validates that the resolved IPs for a host are not private,
// loopback, or otherwise internal. Used as the default ssrfDialGuardFn.
func blockPrivateIPs(ctx context.Context, host string) error {
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("dial: dns resolve %q: %w", host, err)
	}
	for _, ip := range ips {
		if ip.IP.IsLoopback() {
			return fmt.Errorf("dial: %q resolves to loopback %s (blocked for SSRF prevention)", host, ip.IP)
		}
		if isPrivateIP(ip.IP) {
			return fmt.Errorf("dial: %q resolves to private/internal address %s", host, ip.IP)
		}
	}
	return nil
}

// newSSRFSafeHTTPClient returns an HTTP client that validates every redirect
// hop against the SSRF filter, preventing redirect-based bypass of the URL check.
// It also uses a custom dialer that re-validates the resolved IP at connection
// time to defeat DNS rebinding attacks (where a short-TTL record returns a
// public IP during the URL check but a private IP at connect time).
func newSSRFSafeHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, _, err := net.SplitHostPort(addr)
				if err != nil {
					host = addr
				}
				if err := ssrfDialGuardFn(ctx, host); err != nil {
					return nil, err
				}
				return dialer.DialContext(ctx, network, addr)
			},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			if err := validateURLFn(req.URL.String()); err != nil {
				return fmt.Errorf("redirect blocked: %w", err)
			}
			return nil
		},
	}
}

// buildHTTPClient creates an SSRF-safe HTTP client with proxy settings applied
// from the provided ProxyConfig. If the config is empty, it falls back to Go's
// default http.ProxyFromEnvironment behavior.
func buildHTTPClient(proxyConfig config.ProxyConfig) *http.Client {
	client := newSSRFSafeHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		return client
	}

	transport.Proxy = proxyFunc(proxyConfig)
	return client
}

// proxyFunc returns an http.Proxy func that applies ProxyConfig settings.
// If the config is empty, it falls back to http.ProxyFromEnvironment.
func proxyFunc(cfg config.ProxyConfig) func(*http.Request) (*url.URL, error) {
	if cfg.HTTPProxy == "" && cfg.HTTPSProxy == "" {
		return http.ProxyFromEnvironment
	}

	httpProxyURL, err := url.Parse(cfg.HTTPProxy)
	if err != nil && cfg.HTTPProxy != "" {
		slog.Warn("network proxy: invalid HTTP_PROXY URL, skipping proxy config", "url", cfg.HTTPProxy, "err", err)
		return http.ProxyFromEnvironment
	}
	httpsProxyURL, err := url.Parse(cfg.HTTPSProxy)
	if err != nil && cfg.HTTPSProxy != "" {
		slog.Warn("network proxy: invalid HTTPS_PROXY URL, skipping proxy config", "url", cfg.HTTPSProxy, "err", err)
		return http.ProxyFromEnvironment
	}

	// Build NoProxy map for fast lookup.
	noProxySet := make(map[string]bool)
	if cfg.NoProxy != "" {
		for _, host := range strings.Split(cfg.NoProxy, ",") {
			host = strings.TrimSpace(host)
			if host != "" {
				noProxySet[host] = true
			}
		}
	}

	// If NoProxy is NOT set via config, fall back to NO_PROXY env var.
	if envNoProxy := os.Getenv("NO_PROXY"); cfg.NoProxy == "" && envNoProxy != "" {
		for _, host := range strings.Split(envNoProxy, ",") {
			host = strings.TrimSpace(host)
			if host != "" {
				noProxySet[host] = true
			}
		}
	}

	return func(req *http.Request) (*url.URL, error) {
		host := req.URL.Hostname()
		if noProxySet[host] {
			return nil, nil
		}

		// If the request is HTTPS and HTTPSProxy is set, use it.
		if req.URL.Scheme == "https" && httpsProxyURL != nil {
			return httpsProxyURL, nil
		}

		// Use HTTPProxy for HTTP requests, or for HTTPS if no HTTPS-specific proxy.
		if httpProxyURL != nil {
			return httpProxyURL, nil
		}

		// Fall back to environment.
		return http.ProxyFromEnvironment(req)
	}
}
