// Browser CDP extension for Mneme.
//
// Provides browser automation tools via Chrome DevTools Protocol:
//   - browser: navigate and extract content from web pages using a real browser
//   - screenshot: capture a full-page screenshot of a URL
//   - web_fetch: fetch and clean web content with JS rendering
//   - curl: raw HTTP request with full control over method, headers, and body
//
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
package main

import (
	"fmt"
	"os"

	"github.com/simon/mneme/pkg/extsdk"
)

func main() {
	cancel := initCDP()
	defer cancel()

	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "browser-cdp",
		Version:     "0.1.0",
		Description: "CDP-based browser automation: browser, screenshot, web_fetch, curl",
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "browser",
		Description: "Navigate to a URL using a real browser and extract the rendered page content as readable text. Use this when you need JavaScript-rendered content or complex pages.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url":     map[string]interface{}{"type": "string", "description": "URL to navigate to"},
				"timeout": map[string]interface{}{"type": "number", "description": "Page load timeout in seconds (default 15)"},
			},
			"required": []string{"url"},
		},
		Permission: "read_only",
		HasEffects: false,
	}, browserTool)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "screenshot",
		Description: "Take a full-page screenshot of a URL using headless Chrome. Returns the screenshot as a base64-encoded PNG.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url":      map[string]interface{}{"type": "string", "description": "URL to capture"},
				"width":    map[string]interface{}{"type": "number", "description": "Viewport width (default 1280)"},
				"height":   map[string]interface{}{"type": "number", "description": "Viewport height (default 800)"},
				"fullPage": map[string]interface{}{"type": "boolean", "description": "Capture full page (default true)"},
				"selector": map[string]interface{}{"type": "string", "description": "CSS selector to capture a specific element"},
			},
			"required": []string{"url"},
		},
		Permission: "read_only",
		HasEffects: false,
	}, screenshotTool)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "web_fetch",
		Description: "Fetch and clean content from a web page, removing ads, navigation, and boilerplate. Returns the main content as readable text.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url":         map[string]interface{}{"type": "string", "description": "URL to fetch"},
				"maxChars":    map[string]interface{}{"type": "number", "description": "Maximum characters to return (default 10000)"},
				"includeHTML": map[string]interface{}{"type": "boolean", "description": "Include raw HTML in output (default false)"},
			},
			"required": []string{"url"},
		},
		Permission: "read_only",
		HasEffects: false,
	}, webFetchTool)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "curl",
		Description: "Make a raw HTTP request with full control over method, headers, and body. Use for API calls and debugging.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url":     map[string]interface{}{"type": "string", "description": "URL to request"},
				"method":  map[string]interface{}{"type": "string", "description": "HTTP method (GET, POST, PUT, DELETE, etc.)"},
				"headers": map[string]interface{}{"type": "object", "description": "HTTP headers as key-value pairs"},
				"body":    map[string]interface{}{"type": "string", "description": "Request body"},
				"timeout": map[string]interface{}{"type": "number", "description": "Request timeout in seconds (default 30)"},
			},
			"required": []string{"url"},
		},
		Permission: "read_only",
		HasEffects: false,
	}, curlTool)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "browser-cdp: %v\n", err)
		os.Exit(1)
	}
}
