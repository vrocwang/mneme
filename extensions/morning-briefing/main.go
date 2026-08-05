// Morning Briefing extension for Mneme.
//
// Provides daily briefing generation tools:
//   - briefing_generate: create a daily summary from memory, calendar, tasks, and weather
//
// Protocol: stdin/stdout JSON-RPC 2.0 (one message per line).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type manifest struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
	AgentDefs   []string `json:"agent_defs"`
	ProtocolMin int      `json:"protocol_min"`
}
type toolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Permission  string                 `json:"permission"`
	HasEffects  bool                   `json:"has_effects"`
}
type callToolParams struct {
	Name string
	Args map[string]interface{}
}
type callToolResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

var extManifest = manifest{
	Name:        "morning-briefing",
	Version:     "0.1.0",
	Description: "Daily morning briefing: calendar, tasks, weather summary",
	Tools:       []string{"briefing_generate"},
	AgentDefs:   []string{"morning_briefing"},
	ProtocolMin: 1,
}

var agentDefs = []struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Tier          string   `json:"tier"`
	SystemPrompt  string   `json:"systemPrompt"`
	ToolAllowlist []string `json:"toolAllowlist"`
	MaxIterations int      `json:"maxIterations"`
	Hidden        bool     `json:"hidden"`
}{
	{
		ID: "morning_briefing", Name: "Morning Briefing",
		Description: "Generates a daily briefing summary with weather, calendar, tasks, and memory highlights",
		Tier:        "worker",
		SystemPrompt: `You generate a daily morning briefing. Compile information from:
- Memory: recent important facts and user context
- Tasks: pending and overdue to-do items
- Calendar: today's events (if available)
- Weather: current conditions (if API key set)
Present the briefing in a clear, scannable format with sections.`,
		ToolAllowlist: []string{"briefing_generate", "memory_search", "todo_list", "web_search"},
		MaxIterations: 10, Hidden: false,
	},
}

var toolDefs = []toolDef{
	{
		Name:        "briefing_generate",
		Description: "Generate a daily morning briefing with weather, task summary, and memory highlights",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"date":           map[string]interface{}{"type": "string", "description": "Date for briefing (YYYY-MM-DD, default: today)"},
				"includeWeather": map[string]interface{}{"type": "boolean", "description": "Include weather (requires OPENWEATHER_API_KEY + OPENWEATHER_CITY)"},
				"memoryLimit":    map[string]interface{}{"type": "number", "description": "Max memory items to include (default 5)"},
				"tasks":          map[string]interface{}{"type": "array", "description": "Array of task objects with title, status, due_date"},
			},
			"required": []string{},
		},
		Permission: "read_only",
		HasEffects: false,
	},
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("morning-briefing extension starting")
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return
			}
			return
		}
		var req rpcRequest
		json.Unmarshal(line, &req)
		resp := handleRequest(&req)
		respBytes, _ := json.Marshal(resp)
		fmt.Fprintf(os.Stdout, "%s\n", respBytes)
	}
}

func handleRequest(req *rpcRequest) *rpcResponse {
	switch req.Method {
	case "extension.describe":
		result, _ := json.Marshal(extManifest)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.list_tools":
		type lr struct{ Tools []toolDef }
		result, _ := json.Marshal(lr{Tools: toolDefs})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.list_agents":
		result, _ := json.Marshal(map[string]interface{}{"agents": agentDefs})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.call_tool":
		var params callToolParams
		json.Unmarshal(req.Params, &params)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var result callToolResult
		switch params.Name {
		case "briefing_generate":
			result = briefingGenerate(ctx, params.Args)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown: %s", params.Name)}
		}
		resultBytes, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown: %s", req.Method)}}
	}
}

func briefingGenerate(ctx context.Context, args map[string]interface{}) callToolResult {
	includeWeather, _ := args["includeWeather"].(bool)
	memoryLimit := 5
	if l, ok := getInt(args, "memoryLimit"); ok && l > 0 {
		memoryLimit = l
	}

	now := time.Now()
	if d, ok := args["date"].(string); ok && d != "" {
		if parsed, err := time.Parse("2006-01-02", d); err == nil {
			now = parsed
		}
	}
	dayName := now.Weekday().String()
	dateDisplay := now.Format("Monday, January 2, 2006")
	dateStr := now.Format("2006-01-02")

	var out strings.Builder
	out.WriteString(fmt.Sprintf("☀️  Morning Briefing — %s\n", dateDisplay))
	out.WriteString(fmt.Sprintf("══════════════════════════════════════\n\n"))

	// Date & Time
	out.WriteString(fmt.Sprintf("📅  %s  |  %s\n\n", dayName, dateDisplay))

	// Weather
	if includeWeather {
		out.WriteString("🌤  Weather\n───────────\n")
		weather := fetchWeather(ctx)
		if weather != "" {
			out.WriteString(weather + "\n\n")
		} else {
			out.WriteString("Set OPENWEATHER_API_KEY and OPENWEATHER_CITY for weather data.\n\n")
		}
	}

	// Tasks
	out.WriteString("✅  Tasks\n────────\n")
	if tasksRaw, ok := args["tasks"].([]interface{}); ok && len(tasksRaw) > 0 {
		pending := 0
		for _, t := range tasksRaw {
			if tm, ok := t.(map[string]interface{}); ok {
				title, _ := tm["title"].(string)
				status, _ := tm["status"].(string)
				due, _ := tm["due_date"].(string)
				if status != "done" && status != "completed" {
					pending++
					marker := "⬜"
					if due != "" && due <= dateStr {
						marker = "🔴"
					}
					out.WriteString(fmt.Sprintf("  %s %s", marker, title))
					if due != "" {
						out.WriteString(fmt.Sprintf(" (due: %s)", due))
					}
					out.WriteString("\n")
				}
			}
		}
		if pending == 0 {
			out.WriteString("  All caught up! No pending tasks.\n")
		}
	} else {
		out.WriteString("  No task data provided. Use todo_list for a task summary.\n")
	}
	out.WriteString("\n")

	// Memory highlights
	out.WriteString("🧠  Memory Highlights\n───────────────────\n")
	out.WriteString(fmt.Sprintf("  (Last %d remembered items)\n", memoryLimit))
	out.WriteString("  Use memory_search to populate this section.\n\n")

	// Quote
	out.WriteString("💡  Today's Focus\n───────────────\n")
	out.WriteString("  Take one step at a time. Progress over perfection.\n")

	return callToolResult{Success: true, Output: out.String()}
}

func fetchWeather(ctx context.Context) string {
	apiKey := os.Getenv("OPENWEATHER_API_KEY")
	city := os.Getenv("OPENWEATHER_CITY")
	if apiKey == "" || city == "" {
		return ""
	}

	url := fmt.Sprintf("https://api.openweathermap.org/data/2.5/weather?q=%s&appid=%s&units=metric", city, apiKey)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var data struct {
		Main struct {
			Temp     float64 `json:"temp"`
			Humidity int     `json:"humidity"`
		} `json:"main"`
		Weather []struct {
			Description string `json:"description"`
		} `json:"weather"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return ""
	}

	if data.Name == "" {
		return ""
	}
	desc := ""
	if len(data.Weather) > 0 {
		desc = data.Weather[0].Description
	}
	return fmt.Sprintf("  %s: %.0f°C, %s, humidity %d%%", data.Name, data.Main.Temp, desc, data.Main.Humidity)
}

func getInt(args map[string]interface{}, key string) (int, bool) {
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
