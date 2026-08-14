package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/simon/mneme/pkg/extsdk"
)

var (
	allocCtx    context.Context
	allocCancel context.CancelFunc
)

// initCDP initializes the Chrome browser context for reuse across calls.
// Returns a cancel function that should be called on shutdown.
func initCDP() context.CancelFunc {
	opts := append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Headless,
		chromedp.DisableGPU,
		chromedp.WindowSize(1280, 800),
	)

	// Check if Chrome/Chromium is available
	browserPath := os.Getenv("MNEME_CHROME_PATH")
	if browserPath != "" {
		opts = append(opts, chromedp.ExecPath(browserPath))
	}

	var ctx context.Context
	ctx, allocCancel = chromedp.NewExecAllocator(context.Background(), opts...)
	allocCtx = ctx

	slog.Default().Info("CDP browser initialized")
	return func() {
		allocCancel()
		slog.Default().Info("CDP browser shutdown")
	}
}

// hasChrome checks if Chrome is available.
func hasChrome() bool {
	// Try common paths
	paths := []string{
		os.Getenv("MNEME_CHROME_PATH"),
		"/usr/bin/chromium-browser",
		"/usr/bin/chromium",
		"/usr/bin/google-chrome",
		"/usr/bin/google-chrome-stable",
		"/snap/bin/chromium",
	}
	for _, p := range paths {
		if p != "" {
			if _, err := os.Stat(p); err == nil {
				return true
			}
		}
	}
	// Fallback: check PATH
	for _, p := range []string{"chromium-browser", "chromium", "google-chrome", "google-chrome-stable"} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// browserTool navigates to a URL and extracts the rendered page content.
func browserTool(ctx context.Context, args map[string]interface{}) extsdk.Result {
	rawURL, _ := args["url"].(string)
	if rawURL == "" {
		return extsdk.Result{Error: "url is required"}
	}

	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}

	if err := validateSafeURL(rawURL); err != nil {
		return extsdk.Result{Error: fmt.Sprintf("URL rejected: %v", err)}
	}

	if !hasChrome() {
		return extsdk.Result{Error: "Chrome/Chromium not found. Set MNEME_CHROME_PATH or install chromium-browser."}
	}

	timeout := 15.0
	if t, ok := floatFromArgs(args, "timeout"); ok && t > 0 {
		timeout = t
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	tabCtx, tabCancel := chromedp.NewContext(allocCtx)
	defer tabCancel()

	var title, body string
	err := chromedp.Run(tabCtx,
		chromedp.Navigate(rawURL),
		chromedp.WaitReady("body"),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Title(&title),
		chromedp.OuterHTML("html", &body),
	)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("browser navigation: %v", err)}
	}

	text := extractReadableText(body)
	out := fmt.Sprintf("URL: %s\nTitle: %s\n\n%s", rawURL, title, truncateStr(text, 5000))
	if len(text) > 5000 {
		out += fmt.Sprintf("\n\n[Content truncated: %d total characters]", len(text))
	}

	return extsdk.Result{Success: true, Output: out}
}

func floatFromArgs(args map[string]interface{}, key string) (float64, bool) {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return n, true
		case float32:
			return float64(n), true
		case int:
			return float64(n), true
		case int64:
			return float64(n), true
		}
	}
	return 0, false
}
