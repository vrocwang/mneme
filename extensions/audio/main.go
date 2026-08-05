// Audio extension for Mneme.
//
// Provides audio generation tools:
//   - audio_generate_podcast: create a podcast-style audio from text
//   - audio_email: convert email text to audio summary
//   - audio_status: check TTS engine availability
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
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	Name:        "audio",
	Version:     "0.1.0",
	Description: "Audio generation: podcast, email-to-audio, TTS status",
	Tools:       []string{"audio_generate_podcast", "audio_email", "audio_status"},
	AgentDefs:   []string{},
	ProtocolMin: 1,
}

var toolDefs = []toolDef{
	{
		Name:        "audio_generate_podcast",
		Description: "Generate a podcast-style audio file from text. Uses the system TTS engine (say on macOS, espeak/festival on Linux).",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"text":      map[string]interface{}{"type": "string", "description": "Text to convert to speech"},
				"voice":     map[string]interface{}{"type": "string", "description": "Voice name (platform-specific)"},
				"speed":     map[string]interface{}{"type": "number", "description": "Speech speed multiplier (default 1.0)"},
				"outputDir": map[string]interface{}{"type": "string", "description": "Output directory for audio file"},
				"format":    map[string]interface{}{"type": "string", "description": "Output format: wav, mp3 (default wav)"},
			},
			"required": []string{"text"},
		},
		Permission: "write",
		HasEffects: true,
	},
	{
		Name:        "audio_email",
		Description: "Convert an email body to a spoken audio summary",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"subject":   map[string]interface{}{"type": "string", "description": "Email subject"},
				"body":      map[string]interface{}{"type": "string", "description": "Email body text"},
				"outputDir": map[string]interface{}{"type": "string", "description": "Output directory"},
			},
			"required": []string{"body"},
		},
		Permission: "write",
		HasEffects: true,
	},
	{
		Name:        "audio_status",
		Description: "Check available TTS engines and voices on the current system",
		Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Permission:  "read_only",
		HasEffects:  false,
	},
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("audio extension starting")
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
		result, _ := json.Marshal(map[string]interface{}{"agents": []interface{}{}})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.call_tool":
		var params callToolParams
		json.Unmarshal(req.Params, &params)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		var result callToolResult
		switch params.Name {
		case "audio_generate_podcast":
			result = generatePodcast(ctx, params.Args)
		case "audio_email":
			result = generateEmailAudio(ctx, params.Args)
		case "audio_status":
			result = audioStatus()
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown: %s", params.Name)}
		}
		resultBytes, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown: %s", req.Method)}}
	}
}

func ttsEngine() (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("say"); err == nil {
			return "say", []string{"-v", "Samantha"}
		}
	case "linux":
		for _, e := range []string{"espeak", "festival", "flite"} {
			if _, err := exec.LookPath(e); err == nil {
				return e, nil
			}
		}
	case "windows":
		return "powershell", []string{"-Command", `Add-Type -AssemblyName System.Speech; (New-Object System.Speech.Synthesis.SpeechSynthesizer).Speak($args[0])`}
	}
	return "", nil
}

func generatePodcast(ctx context.Context, args map[string]interface{}) callToolResult {
	text, _ := args["text"].(string)
	if text == "" {
		return callToolResult{Error: "text is required"}
	}

	engine, extraArgs := ttsEngine()
	if engine == "" {
		return callToolResult{Error: "No TTS engine found. Install espeak (sudo apt install espeak) or festival."}
	}

	outputDir := os.TempDir()
	if d, ok := args["outputDir"].(string); ok && d != "" {
		outputDir = d
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return callToolResult{Error: fmt.Sprintf("mkdir output dir: %v", err)}
	}

	format := "wav"
	if f, ok := args["format"].(string); ok && f != "" {
		format = f
	}
	outPath := filepath.Join(outputDir, fmt.Sprintf("podcast-%d.%s", time.Now().UnixMilli(), format))

	var cmd *exec.Cmd
	switch engine {
	case "say":
		args_ := append(extraArgs, text, "-o", outPath)
		if format == "wav" {
			args_ = append(args_, "--data-format=LEI16@22050")
		}
		cmd = exec.CommandContext(ctx, engine, args_...)
	case "espeak":
		cmd = exec.CommandContext(ctx, engine, "-w", outPath, text)
	case "festival":
		cmd = exec.CommandContext(ctx, engine, "--tts")
		cmd.Stdin = strings.NewReader(text)
	default:
		cmd = exec.CommandContext(ctx, engine, append(extraArgs, text)...)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("TTS generation: %v (%s)", err, string(out))}
	}

	abs, _ := filepath.Abs(outPath)
	return callToolResult{Success: true, Output: fmt.Sprintf("Podcast audio generated: %s\nEngine: %s\nSize: check file", abs, engine)}
}

func generateEmailAudio(ctx context.Context, args map[string]interface{}) callToolResult {
	subject, _ := args["subject"].(string)
	body, _ := args["body"].(string)
	if body == "" {
		return callToolResult{Error: "body is required"}
	}

	summary := fmt.Sprintf("Email received. Subject: %s. Body: %s", subject, body)
	summary = truncateRunes(summary, 2000)

	return generatePodcast(ctx, map[string]interface{}{
		"text":      summary,
		"outputDir": args["outputDir"],
		"format":    "wav",
	})
}

func audioStatus() callToolResult {
	engine, args := ttsEngine()
	status := map[string]interface{}{
		"platform":   runtime.GOOS,
		"tts_engine": engine,
		"available":  engine != "",
		"args":       args,
	}

	// Check additional engines
	engines := []string{}
	for _, e := range []string{"espeak", "festival", "flite", "say", "ffmpeg"} {
		if _, err := exec.LookPath(e); err == nil {
			engines = append(engines, e)
		}
	}
	status["installed_engines"] = engines

	b, _ := json.MarshalIndent(status, "", "  ")
	return callToolResult{Success: true, Output: string(b)}
}

func truncateRunes(s string, maxRunes int) string {
	if len(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}
