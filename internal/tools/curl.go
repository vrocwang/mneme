package tools

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/simon/mneme/internal/config"
)

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

	cmdArgs := []string{"-s", "-S"}

	// Proxy: if HTTPProxy is configured, pass --proxy to curl.
	if t.proxyConfig.HTTPProxy != "" {
		cmdArgs = append(cmdArgs, "--proxy", t.proxyConfig.HTTPProxy)
	}

	// Follow redirects by default.
	followRedirects := true
	if v, ok := args["follow_redirects"].(bool); ok {
		followRedirects = v
	}
	if followRedirects {
		cmdArgs = append(cmdArgs, "-L")
	}

	// Timeout.
	if v, ok := args["timeout_secs"].(float64); ok && v > 0 {
		cmdArgs = append(cmdArgs, "--max-time", fmt.Sprintf("%.0f", v))
	} else {
		cmdArgs = append(cmdArgs, "--max-time", "30")
	}

	if method != "" {
		cmdArgs = append(cmdArgs, "-X", method)
	}

	// Headers (only string values).
	if hdrs, ok := args["headers"].(map[string]interface{}); ok {
		for k, v := range hdrs {
			if vs, ok := v.(string); ok {
				cmdArgs = append(cmdArgs, "-H", fmt.Sprintf("%s: %s", k, vs))
			}
		}
	}

	// Body.
	if body, ok := args["body"].(string); ok && body != "" {
		cmdArgs = append(cmdArgs, "-d", body)
	}

	cmdArgs = append(cmdArgs, rawURL)
	cmd := exec.CommandContext(ctx, "curl", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return Result{Error: fmt.Sprintf("curl failed: %v — %s", err, safeTruncate(string(out), 1000))}
	}

	return Result{Success: true, Output: safeTruncate(string(out), 100000)}
}
