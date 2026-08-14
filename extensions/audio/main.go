// Audio extension for Mneme.
//
// Provides audio generation tools:
//   - audio_generate_podcast: create a podcast-style audio from text
//   - audio_email: convert email text to audio summary
//   - audio_status: check TTS engine availability
//
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/simon/mneme/pkg/extsdk"
)

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "audio",
		Version:     "0.1.0",
		Description: "Audio generation: podcast, email-to-audio, TTS status",
	})

	srv.RegisterTool(extsdk.ToolDef{
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
	}, generatePodcast)

	srv.RegisterTool(extsdk.ToolDef{
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
	}, generateEmailAudio)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "audio_status",
		Description: "Check available TTS engines and voices on the current system",
		Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Permission:  "read_only",
		HasEffects:  false,
	}, func(ctx context.Context, args map[string]interface{}) extsdk.Result {
		return audioStatus()
	})

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "audio: %v\n", err)
		os.Exit(1)
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

func generatePodcast(ctx context.Context, args map[string]interface{}) extsdk.Result {
	text, _ := args["text"].(string)
	if text == "" {
		return extsdk.Result{Error: "text is required"}
	}

	engine, extraArgs := ttsEngine()
	if engine == "" {
		return extsdk.Result{Error: "No TTS engine found. Install espeak (sudo apt install espeak) or festival."}
	}

	outputDir := os.TempDir()
	if d, ok := args["outputDir"].(string); ok && d != "" {
		outputDir = d
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return extsdk.Result{Error: fmt.Sprintf("mkdir output dir: %v", err)}
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
		return extsdk.Result{Error: fmt.Sprintf("TTS generation: %v (%s)", err, string(out))}
	}

	abs, _ := filepath.Abs(outPath)
	return extsdk.Result{Success: true, Output: fmt.Sprintf("Podcast audio generated: %s\nEngine: %s\nSize: check file", abs, engine)}
}

func generateEmailAudio(ctx context.Context, args map[string]interface{}) extsdk.Result {
	subject, _ := args["subject"].(string)
	body, _ := args["body"].(string)
	if body == "" {
		return extsdk.Result{Error: "body is required"}
	}

	summary := fmt.Sprintf("Email received. Subject: %s. Body: %s", subject, body)
	summary = truncateRunes(summary, 2000)

	return generatePodcast(ctx, map[string]interface{}{
		"text":      summary,
		"outputDir": args["outputDir"],
		"format":    "wav",
	})
}

func audioStatus() extsdk.Result {
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
	return extsdk.Result{Success: true, Output: string(b)}
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
