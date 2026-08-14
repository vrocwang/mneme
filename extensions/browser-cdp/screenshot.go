package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/simon/mneme/pkg/extsdk"
)

// screenshotTool takes a screenshot of a URL using headless Chrome.
func screenshotTool(ctx context.Context, args map[string]interface{}) extsdk.Result {
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

	width := 1280
	if w, ok := intFromArgs(args, "width"); ok && w > 0 {
		width = w
	}
	height := 800
	if h, ok := intFromArgs(args, "height"); ok && h > 0 {
		height = h
	}

	fullPage := true
	if fp, ok := args["fullPage"].(bool); ok {
		fullPage = fp
	}

	selector, hasSelector := args["selector"].(string)

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	tabCtx, tabCancel := chromedp.NewContext(allocCtx)
	defer tabCancel()

	var screenshot []byte

	actions := []chromedp.Action{
		chromedp.EmulateViewport(int64(width), int64(height)),
		chromedp.Navigate(rawURL),
		chromedp.WaitReady("body"),
		chromedp.Sleep(500 * time.Millisecond),
	}

	if hasSelector && selector != "" {
		actions = append(actions,
			chromedp.Screenshot(selector, &screenshot, chromedp.NodeVisible),
		)
	} else if fullPage {
		actions = append(actions,
			chromedp.FullScreenshot(&screenshot, 90),
		)
	} else {
		actions = append(actions,
			chromedp.CaptureScreenshot(&screenshot),
		)
	}

	if err := chromedp.Run(tabCtx, actions...); err != nil {
		return extsdk.Result{Error: fmt.Sprintf("screenshot: %v", err)}
	}

	b64 := base64.StdEncoding.EncodeToString(screenshot)

	var out strings.Builder
	out.WriteString(fmt.Sprintf("URL: %s\n", rawURL))
	out.WriteString(fmt.Sprintf("Size: %d bytes (base64)\n", len(b64)))
	out.WriteString(fmt.Sprintf("Dimensions: %dx%d\n", width, height))
	out.WriteString(fmt.Sprintf("Format: %s\n\n", "image/png;base64"))
	out.WriteString(b64)

	return extsdk.Result{Success: true, Output: out.String()}
}

func intFromArgs(args map[string]interface{}, key string) (int, bool) {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n), true
		case int:
			return n, true
		case int64:
			return int(n), true
		}
	}
	return 0, false
}
