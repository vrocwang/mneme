package tools

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// URLGuard validates URLs for safety before they are accessed by other tools.
type URLGuard struct {
	BaseTool
}

func NewURLGuard() *URLGuard {
	return &URLGuard{
		BaseTool: BaseTool{
			SchemaVal: Schema{
				Name:        "url_guard",
				Description: "Validate a URL for safety. Checks protocol, credentials, and DNS resolution for private/internal IPs.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"url": map[string]interface{}{
							"type":        "string",
							"description": "URL to validate",
						},
					},
					"required": []string{"url"},
				},
			},
			PermLevel:      PermReadOnly,
			HasSideEffects: false,
		},
	}
}

func (t *URLGuard) Schema() Schema { return t.BaseTool.SchemaVal }

func (t *URLGuard) Execute(ctx context.Context, args map[string]interface{}) Result {
	if err := ctx.Err(); err != nil {
		return Result{Error: err.Error()}
	}

	rawURL, _ := args["url"].(string)
	if rawURL == "" {
		return Result{Error: "url is required"}
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" {
		return Result{Success: true, Output: fmt.Sprintf("blocked: invalid or missing URL scheme in %q", rawURL)}
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return Result{Success: true, Output: fmt.Sprintf("blocked: unsupported protocol %q", scheme)}
	}

	if parsed.User != nil {
		return Result{Success: true, Output: "blocked: URL contains embedded credentials"}
	}

	host := parsed.Hostname()

	// If host is already an IP, validate directly.
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			return Result{Success: true, Output: fmt.Sprintf("blocked: URL %s resolves to private IP %s", rawURL, ip.String())}
		}
		return Result{Success: true, Output: fmt.Sprintf("safe: %s (IP: %s)", rawURL, ip.String())}
	}

	// Use context-aware DNS resolution.
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return Result{Success: true, Output: fmt.Sprintf("warning: DNS lookup failed for %s: %v", host, err)}
	}

	var blockedIPs []string
	for _, addr := range ips {
		if isPrivateIP(addr.IP) {
			blockedIPs = append(blockedIPs, addr.IP.String())
		}
	}

	if len(blockedIPs) > 0 {
		return Result{Success: true, Output: fmt.Sprintf("blocked: URL %s resolves to private/internal IPs: %s", rawURL, strings.Join(blockedIPs, ", "))}
	}

	return Result{Success: true, Output: fmt.Sprintf("safe: %s (resolves to %d public IPs)", rawURL, len(ips))}
}
