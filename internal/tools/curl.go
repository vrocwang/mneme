package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/simon/mneme/internal/config"
)

// maxCurlTimeoutSecs caps the user-supplied timeout so the tool cannot be used
// to hold connections open indefinitely.
const maxCurlTimeoutSecs = 120

// Curl performs HTTP requests with full control over method, headers, and body.
type Curl struct {
	BaseTool
	proxyConfig config.ProxyConfig
}

func NewCurl(proxyConfig config.ProxyConfig) *Curl {
	return &Curl{
		proxyConfig: proxyConfig,
		BaseTool: BaseTool{
			SchemaVal: Schema{
				Name:        "curl",
				Description: "Perform an HTTP request with full control over method, headers, and body. Follows redirects by default.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"url": map[string]interface{}{
							"type":        "string",
							"description": "URL to request",
						},
						"method": map[string]interface{}{
							"type":        "string",
							"description": "HTTP method (GET, POST, PUT, DELETE, etc.)",
						},
						"headers": map[string]interface{}{
							"type":                 "object",
							"description":          "HTTP headers as string key-value pairs",
							"additionalProperties": map[string]interface{}{"type": "string"},
						},
						"body": map[string]interface{}{
							"type":        "string",
							"description": "Request body",
						},
						"follow_redirects": map[string]interface{}{
							"type":        "boolean",
							"description": "Follow HTTP redirects (default true)",
						},
						"timeout_secs": map[string]interface{}{
							"type":        "integer",
							"description": "Request timeout in seconds (default 30)",
						},
					},
					"required": []string{"url"},
				},
			},
			PermLevel:      PermExecute,
			HasSideEffects: true,
		},
	}
}

func (t *Curl) Schema() Schema { return t.BaseTool.SchemaVal }

func (t *Curl) Execute(ctx context.Context, args map[string]interface{}) Result {
	if err := ctx.Err(); err != nil {
		return Result{Error: err.Error()}
	}

	rawURL, _ := args["url"].(string)
	if rawURL == "" {
		return Result{Error: "url is required"}
	}

	if err := validateURLFn(rawURL); err != nil {
		return Result{Error: fmt.Sprintf("url rejected: %v", err)}
	}

	method, _ := args["method"].(string)
	if method == "" {
		method = http.MethodGet
	}

	var bodyReader io.Reader
	if body, ok := args["body"].(string); ok && body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
	if err != nil {
		return Result{Error: fmt.Sprintf("create request: %v", err)}
	}

	// Headers (only string values).
	if hdrs, ok := args["headers"].(map[string]interface{}); ok {
		for k, v := range hdrs {
			if vs, ok := v.(string); ok {
				req.Header.Set(k, vs)
			}
		}
	}

	// Build an SSRF-safe client: it re-validates the resolved IP at dial time
	// (defeating DNS rebinding) and re-validates every redirect hop. It also
	// applies the configured proxy settings.
	client := buildHTTPClient(t.proxyConfig)

	// Follow redirects by default; the SSRF-safe client re-validates each hop.
	followRedirects := true
	if v, ok := args["follow_redirects"].(bool); ok {
		followRedirects = v
	}
	if !followRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	// Clamp the timeout to a bounded value.
	timeoutSecs := 30
	if v, ok := args["timeout_secs"].(float64); ok && v > 0 {
		timeoutSecs = int(v)
	}
	if timeoutSecs > maxCurlTimeoutSecs {
		timeoutSecs = maxCurlTimeoutSecs
	}
	client.Timeout = time.Duration(timeoutSecs) * time.Second

	resp, err := client.Do(req)
	if err != nil {
		return Result{Error: fmt.Sprintf("curl failed: %v", err)}
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return Result{Error: fmt.Sprintf("read response: %v", err)}
	}

	out := fmt.Sprintf("Status: %d\n%s", resp.StatusCode, string(bodyBytes))
	return Result{Success: true, Output: safeTruncate(out, 100000)}
}
