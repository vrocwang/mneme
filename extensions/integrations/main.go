// Integrations extension for Mneme.
//
// Provides third-party integration tools:
//   - composio_list_actions: list available Composio actions
//   - composio_execute: execute a Composio action
//   - apify_run: run an Apify actor
//   - google_places_search: search Google Places
//   - stocks_lookup: get stock quote
//   - twilio_send_sms: send SMS via Twilio
//
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"strings"
	"time"

	"github.com/simon/mneme/pkg/extsdk"
)

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "integrations",
		Version:     "0.1.0",
		Description: "Third-party integrations: Composio, Apify, Google Places, Stocks, Twilio",
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "composio_list_actions",
		Description: "List available actions from Composio (a tool/action registry). Requires COMPOSIO_API_KEY.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"appName": map[string]interface{}{"type": "string", "description": "Filter by app name (e.g. 'github', 'slack', 'gmail')"},
				"limit":   map[string]interface{}{"type": "number", "description": "Max results (default 50)"},
			},
			"required": []string{},
		},
		Permission: "read_only",
		HasEffects: false,
	}, composioList)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "composio_execute",
		Description: "Execute a Composio action. Requires COMPOSIO_API_KEY.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"actionId": map[string]interface{}{"type": "string", "description": "Composio action ID (e.g. 'github_create_issue')"},
				"params":   map[string]interface{}{"type": "object", "description": "Action parameters as key-value pairs"},
				"authId":   map[string]interface{}{"type": "string", "description": "Connected account ID"},
			},
			"required": []string{"actionId", "params"},
		},
		Permission: "execute",
		HasEffects: true,
	}, composioExecute)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "apify_run",
		Description: "Run an Apify actor. Requires APIFY_API_TOKEN.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"actorId": map[string]interface{}{"type": "string", "description": "Apify actor ID (e.g. 'apify/web-scraper')"},
				"input":   map[string]interface{}{"type": "object", "description": "Actor input parameters"},
				"wait":    map[string]interface{}{"type": "boolean", "description": "Wait for completion (default true)"},
				"timeout": map[string]interface{}{"type": "number", "description": "Timeout in seconds (default 120)"},
			},
			"required": []string{"actorId", "input"},
		},
		Permission: "execute",
		HasEffects: true,
	}, apifyRun)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "apify_get_run_status",
		Description: "Check the status of a running Apify actor. Requires APIFY_API_TOKEN.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"runId": map[string]interface{}{"type": "string", "description": "Apify run ID"},
			},
			"required": []string{"runId"},
		},
		Permission: "read_only",
		HasEffects: false,
	}, apifyGetRunStatus)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "apify_get_run_results",
		Description: "Fetch results from a completed Apify actor run. Requires APIFY_API_TOKEN.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"runId": map[string]interface{}{"type": "string", "description": "Apify run ID"},
				"limit": map[string]interface{}{"type": "number", "description": "Max results (default 100)"},
			},
			"required": []string{"runId"},
		},
		Permission: "read_only",
		HasEffects: false,
	}, apifyGetRunResults)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "google_places_search",
		Description: "Search for places using Google Places API. Requires GOOGLE_PLACES_API_KEY.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query":    map[string]interface{}{"type": "string", "description": "Search query (e.g. 'coffee shops near me')"},
				"location": map[string]interface{}{"type": "string", "description": "Lat,Lng for location bias (optional)"},
				"radius":   map[string]interface{}{"type": "number", "description": "Search radius in meters (optional)"},
				"limit":    map[string]interface{}{"type": "number", "description": "Max results (default 10)"},
			},
			"required": []string{"query"},
		},
		Permission: "read_only",
		HasEffects: false,
	}, googlePlaces)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "stocks_lookup",
		Description: "Look up current stock price and basic info by ticker symbol",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"symbol": map[string]interface{}{"type": "string", "description": "Stock ticker symbol (e.g. AAPL, GOOGL, TSLA)"},
			},
			"required": []string{"symbol"},
		},
		Permission: "read_only",
		HasEffects: false,
	}, stocksLookup)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "twilio_send_sms",
		Description: "Send an SMS message via Twilio. Requires TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN, and TWILIO_PHONE_NUMBER env vars.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"to":   map[string]interface{}{"type": "string", "description": "Recipient phone number (E.164 format: +1234567890)"},
				"body": map[string]interface{}{"type": "string", "description": "Message body (max 1600 chars)"},
			},
			"required": []string{"to", "body"},
		},
		Permission: "execute",
		HasEffects: true,
	}, twilioSMS)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "integrations: %v\n", err)
		os.Exit(1)
	}
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

// ── Composio ──────────────────────────────────────────────────────

const composioAPI = "https://backend.composio.dev/api/v2"

func composioList(ctx context.Context, args map[string]interface{}) extsdk.Result {
	apiKey := os.Getenv("COMPOSIO_API_KEY")
	if apiKey == "" {
		return extsdk.Result{Error: "COMPOSIO_API_KEY not set"}
	}
	appName, _ := args["appName"].(string)
	url := composioAPI + "/actions"
	if appName != "" {
		url += "?appName=" + appName
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("X-API-Key", apiKey)
	resp, err := httpClient.Do(req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("composio: %v", err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return extsdk.Result{Success: resp.StatusCode < 400, Output: formatJSON(string(body))}
}

func composioExecute(ctx context.Context, args map[string]interface{}) extsdk.Result {
	apiKey := os.Getenv("COMPOSIO_API_KEY")
	if apiKey == "" {
		return extsdk.Result{Error: "COMPOSIO_API_KEY not set"}
	}
	actionID, _ := args["actionId"].(string)
	params, _ := args["params"].(map[string]interface{})
	authID, _ := args["authId"].(string)

	payload := map[string]interface{}{"params": params}
	if authID != "" {
		payload["connectedAccountId"] = authID
	}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", composioAPI+"/actions/"+neturl.PathEscape(actionID)+"/execute", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	resp, err := httpClient.Do(req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("composio execute: %v", err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return extsdk.Result{Success: resp.StatusCode < 400, Output: formatJSON(string(body))}
}

// ── Apify ─────────────────────────────────────────────────────────

func apifyRun(ctx context.Context, args map[string]interface{}) extsdk.Result {
	apiToken := os.Getenv("APIFY_API_TOKEN")
	if apiToken == "" {
		return extsdk.Result{Error: "APIFY_API_TOKEN not set"}
	}
	actorID, _ := args["actorId"].(string)
	input, _ := args["input"].(map[string]interface{})

	payload := map[string]interface{}{
		"input": input,
	}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("https://api.apify.com/v2/acts/%s/runs?token=%s", neturl.PathEscape(actorID), neturl.QueryEscape(apiToken)),
		bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("apify: %v", err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return extsdk.Result{Success: resp.StatusCode < 400, Output: formatJSON(string(body))}
}

func apifyGetRunStatus(ctx context.Context, args map[string]interface{}) extsdk.Result {
	apiToken := os.Getenv("APIFY_API_TOKEN")
	if apiToken == "" {
		return extsdk.Result{Error: "APIFY_API_TOKEN not set"}
	}
	runID, _ := args["runId"].(string)
	if runID == "" {
		return extsdk.Result{Error: "runId is required"}
	}
	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("https://api.apify.com/v2/actor-runs/%s?token=%s", neturl.PathEscape(runID), neturl.QueryEscape(apiToken)), nil)
	resp, err := httpClient.Do(req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("apify status: %v", err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return extsdk.Result{Success: resp.StatusCode < 400, Output: formatJSON(string(body))}
}

func apifyGetRunResults(ctx context.Context, args map[string]interface{}) extsdk.Result {
	apiToken := os.Getenv("APIFY_API_TOKEN")
	if apiToken == "" {
		return extsdk.Result{Error: "APIFY_API_TOKEN not set"}
	}
	runID, _ := args["runId"].(string)
	if runID == "" {
		return extsdk.Result{Error: "runId is required"}
	}
	limit := 100
	if l, ok := intFrom(args, "limit"); ok && l > 0 {
		limit = l
	}
	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("https://api.apify.com/v2/actor-runs/%s/dataset/items?token=%s&limit=%d", neturl.PathEscape(runID), neturl.QueryEscape(apiToken), limit), nil)
	resp, err := httpClient.Do(req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("apify results: %v", err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return extsdk.Result{Success: resp.StatusCode < 400, Output: formatJSON(string(body))}
}

// ── Google Places ─────────────────────────────────────────────────

func googlePlaces(ctx context.Context, args map[string]interface{}) extsdk.Result {
	apiKey := os.Getenv("GOOGLE_PLACES_API_KEY")
	if apiKey == "" {
		return extsdk.Result{Error: "GOOGLE_PLACES_API_KEY not set"}
	}
	query, _ := args["query"].(string)
	if query == "" {
		return extsdk.Result{Error: "query is required"}
	}
	url := fmt.Sprintf("https://maps.googleapis.com/maps/api/place/textsearch/json?query=%s&key=%s",
		strings.ReplaceAll(query, " ", "+"), apiKey)
	location, _ := args["location"].(string)
	if location != "" {
		url += "&location=" + location
	}
	if radius, ok := intFrom(args, "radius"); ok && radius > 0 {
		url += fmt.Sprintf("&radius=%d", radius)
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := httpClient.Do(req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("places: %v", err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return extsdk.Result{Success: resp.StatusCode < 400, Output: formatJSON(string(body))}
}

// ── Stocks ────────────────────────────────────────────────────────

func stocksLookup(ctx context.Context, args map[string]interface{}) extsdk.Result {
	symbol, _ := args["symbol"].(string)
	if symbol == "" {
		return extsdk.Result{Error: "symbol is required"}
	}
	symbol = strings.ToUpper(symbol)

	// Use a free API endpoint
	url := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?interval=1d&range=1d", symbol)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := httpClient.Do(req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("stocks: %v", err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var data struct {
		Chart struct {
			Result []struct {
				Meta struct {
					Symbol             string  `json:"symbol"`
					RegularMarketPrice float64 `json:"regularMarketPrice"`
					PreviousClose      float64 `json:"previousClose"`
					Currency           string  `json:"currency"`
				} `json:"meta"`
			} `json:"result"`
		} `json:"chart"`
	}
	json.Unmarshal(body, &data)

	if len(data.Chart.Result) == 0 {
		return extsdk.Result{Error: fmt.Sprintf("no data for symbol: %s", symbol)}
	}

	m := data.Chart.Result[0].Meta
	change := m.RegularMarketPrice - m.PreviousClose
	changePct := (change / m.PreviousClose) * 100

	output := fmt.Sprintf("%s: %s%.2f (change: %+.2f / %+.2f%%)\nPrevious close: %s%.2f",
		m.Symbol, m.Currency, m.RegularMarketPrice,
		change, changePct,
		m.Currency, m.PreviousClose)
	return extsdk.Result{Success: true, Output: output}
}

// ── Twilio SMS ────────────────────────────────────────────────────

func twilioSMS(ctx context.Context, args map[string]interface{}) extsdk.Result {
	sid := os.Getenv("TWILIO_ACCOUNT_SID")
	token := os.Getenv("TWILIO_AUTH_TOKEN")
	from := os.Getenv("TWILIO_PHONE_NUMBER")
	if sid == "" || token == "" || from == "" {
		return extsdk.Result{Error: "TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN, and TWILIO_PHONE_NUMBER must be set"}
	}
	to, _ := args["to"].(string)
	body, _ := args["body"].(string)
	if to == "" || body == "" {
		return extsdk.Result{Error: "to and body are required"}
	}

	url := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", sid)
	form := neturl.Values{}
	form.Set("From", from)
	form.Set("To", to)
	form.Set("Body", body)
	payload := form.Encode()
	req, _ := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(payload))
	req.SetBasicAuth(sid, token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("twilio: %v", err)}
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return extsdk.Result{Success: resp.StatusCode < 400, Output: formatJSON(string(respBody))}
}

// ── Helpers ──────────────────────────────────────────────────────

func intFrom(args map[string]interface{}, key string) (int, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

func formatJSON(s string) string {
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
