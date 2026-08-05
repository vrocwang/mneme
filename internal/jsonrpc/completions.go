package jsonrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/simon/mneme/internal/inference"
)

// ── OpenAI-compatible types ────────────────────────────────────────────────

type completionsRequest struct {
	Model       string            `json:"model"`
	Messages    []completionsMsg  `json:"messages"`
	Stream      bool              `json:"stream,omitempty"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Temperature float64           `json:"temperature,omitempty"`
	Tools       []completionsTool `json:"tools,omitempty"`
}

type completionsMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type completionsTool struct {
	Type     string      `json:"type"`
	Function functionDef `json:"function"`
}

type functionDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

type completionsResponse struct {
	ID      string     `json:"id"`
	Object  string     `json:"object"`
	Created int64      `json:"created"`
	Model   string     `json:"model"`
	Choices []choice   `json:"choices"`
	Usage   *usageInfo `json:"usage,omitempty"`
}

type choice struct {
	Index        int            `json:"index"`
	Message      *choiceMessage `json:"message,omitempty"`
	Delta        *choiceDelta   `json:"delta,omitempty"`
	FinishReason string         `json:"finish_reason,omitempty"`
}

type choiceMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type choiceDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type usageInfo struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type completionsChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []chunkChoice `json:"choices"`
}

type chunkChoice struct {
	Index        int         `json:"index"`
	Delta        choiceDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

func (s *Server) handleCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "failed to read body")
		return
	}

	var req completionsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if s.provider == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "no provider configured")
		return
	}

	// Build internal chat request.
	messages := make([]inference.Message, len(req.Messages))
	for i, m := range req.Messages {
		messages[i] = inference.Message{Role: m.Role, Content: m.Content}
	}

	chatReq := inference.ChatRequest{
		Model:       req.Model,
		Messages:    messages,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}

	if req.Stream {
		s.handleStreaming(w, r, chatReq)
		return
	}

	s.handleNonStreaming(w, r, chatReq, req.Model)
}

func (s *Server) handleNonStreaming(w http.ResponseWriter, r *http.Request, req inference.ChatRequest, model string) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	tokensCh, errCh := s.provider.Chat(ctx, req)

	var fullText string
	var usage *inference.Usage

	for {
		select {
		case tok, ok := <-tokensCh:
			if !ok {
				goto done
			}
			fullText += tok.Text
			if tok.Usage != nil {
				usage = tok.Usage
			}
		case err := <-errCh:
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
		case <-ctx.Done():
			writeJSONError(w, http.StatusRequestTimeout, "request cancelled")
			return
		}
	}

done:
	resp := completionsResponse{
		ID:      "chatcmpl-" + model,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []choice{{
			Index: 0,
			Message: &choiceMessage{
				Role:    "assistant",
				Content: fullText,
			},
			FinishReason: "stop",
		}},
	}
	if usage != nil {
		resp.Usage = &usageInfo{
			PromptTokens:     usage.InputTokens,
			CompletionTokens: usage.OutputTokens,
			TotalTokens:      usage.InputTokens + usage.OutputTokens,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleStreaming(w http.ResponseWriter, r *http.Request, req inference.ChatRequest) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	flusher, ok := w.(http.Flusher)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "streaming not supported"})
		return
	}

	tokensCh, errCh := s.provider.Chat(ctx, req)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	created := time.Now().Unix()
	firstChunk := true

	for {
		select {
		case tok, ok := <-tokensCh:
			if !ok {
				// Send final [DONE] chunk.
				writeSSEChunk(w, flusher, completionsChunk{
					ID:      "chatcmpl-" + req.Model,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   req.Model,
					Choices: []chunkChoice{{
						Index:        0,
						Delta:        choiceDelta{},
						FinishReason: strPtr("stop"),
					}},
				})
				fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				return
			}

			chunk := completionsChunk{
				ID:      "chatcmpl-" + req.Model,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   req.Model,
				Choices: []chunkChoice{{
					Index: 0,
					Delta: choiceDelta{
						Content: tok.Text,
					},
				}},
			}

			if firstChunk {
				chunk.Choices[0].Delta.Role = "assistant"
				firstChunk = false
			}

			writeSSEChunk(w, flusher, chunk)
		case err := <-errCh:
			if err != nil {
				s.log.Warn("completions streaming error", "error", err)
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func writeSSEChunk(w http.ResponseWriter, flusher http.Flusher, chunk completionsChunk) {
	data, _ := json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

func strPtr(s string) *string { return &s }

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
