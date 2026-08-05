package voice

import (
	"bytes"
	"context"
	"encoding/json"

	"fmt"
	"github.com/simon/mneme/internal/config"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// OpenAITts calls an OpenAI-compatible /v1/audio/speech endpoint.
type OpenAITts struct {
	endpoint string
	apiKey   string
	model    string
	voice    string
	client   *http.Client
}

// NewOpenAITts creates an OpenAI-compatible TTS engine.
func NewOpenAITts(endpoint, apiKey, model, voice string) *OpenAITts {
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "tts-1"
	}
	if voice == "" {
		voice = "alloy"
	}
	return &OpenAITts{
		endpoint: strings.TrimRight(endpoint, "/"),
		apiKey:   apiKey,
		model:    model,
		voice:    voice,
		client:   &http.Client{},
	}
}

func (o *OpenAITts) Name() string { return "openai" }

func (o *OpenAITts) Speak(ctx context.Context, text string) error {
	audioPath := filepath.Join(config.TempDir(), "tts_output.mp3")
	return o.SpeakToFile(ctx, text, audioPath)
}

func (o *OpenAITts) SpeakToFile(ctx context.Context, text, outputPath string) error {
	reqBody := map[string]interface{}{
		"model": o.model,
		"voice": o.voice,
		"input": text,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("openai tts: marshal request: %w", err)
	}

	url := o.endpoint + "/audio/speech"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("openai tts: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("openai tts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("openai tts: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("openai tts: create output dir: %w", err)
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("openai tts: create output file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("openai tts: write output: %w", err)
	}

	return playAudioFile(ctx, outputPath)
}

// playAudioFile plays the given audio file using the default platform player.
func playAudioFile(ctx context.Context, path string) error {
	player := platformPlayCommand()
	if player == "" {
		return fmt.Errorf("no audio player found on this platform")
	}
	return exec.CommandContext(ctx, player, path).Run()
}
