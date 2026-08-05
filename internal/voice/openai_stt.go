package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// OpenAIStt calls an OpenAI-compatible /v1/audio/transcriptions endpoint.
type OpenAIStt struct {
	endpoint string
	apiKey   string
	model    string
	client   *http.Client
}

// NewOpenAIStt creates an OpenAI-compatible STT engine.
func NewOpenAIStt(endpoint, apiKey, model string) *OpenAIStt {
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "whisper-1"
	}
	return &OpenAIStt{
		endpoint: strings.TrimRight(endpoint, "/"),
		apiKey:   apiKey,
		model:    model,
		client:   &http.Client{},
	}
}

func (o *OpenAIStt) Name() string { return "openai" }

func (o *OpenAIStt) TranscribeBytes(ctx context.Context, audioData []byte, format string) (*STTResult, error) {
	if format == "" {
		format = "wav"
	}
	fileName := "audio." + format

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return nil, fmt.Errorf("openai stt: create form file: %w", err)
	}
	if _, err := part.Write(audioData); err != nil {
		return nil, fmt.Errorf("openai stt: write audio data: %w", err)
	}
	writer.WriteField("model", o.model)
	writer.WriteField("response_format", "json")
	writer.Close()

	url := o.endpoint + "/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, fmt.Errorf("openai stt: create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai stt: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai stt: read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("openai stt: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("openai stt: parse response: %w", err)
	}

	return &STTResult{Text: strings.TrimSpace(result.Text), Confidence: 0.95}, nil
}

func (o *OpenAIStt) Transcribe(ctx context.Context, audioPath string) (*STTResult, error) {
	file, err := os.Open(audioPath)
	if err != nil {
		return nil, fmt.Errorf("openai stt: open audio file: %w", err)
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return nil, fmt.Errorf("openai stt: create form file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("openai stt: copy audio data: %w", err)
	}
	writer.WriteField("model", o.model)
	writer.WriteField("response_format", "json")
	writer.Close()

	url := o.endpoint + "/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, fmt.Errorf("openai stt: create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai stt: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai stt: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("openai stt: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("openai stt: parse response: %w", err)
	}

	return &STTResult{Text: strings.TrimSpace(result.Text), Confidence: 0.95}, nil
}
