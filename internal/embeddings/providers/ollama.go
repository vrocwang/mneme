package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type Ollama struct {
	baseURL string
	client  *http.Client
	model   string
}

func NewOllama(baseURL string) *Ollama {
	return &Ollama{baseURL: strings.TrimRight(baseURL, "/"), client: &http.Client{}, model: "nomic-embed-text"}
}

func (p *Ollama) Name() string    { return "ollama" }
func (p *Ollama) Dimensions() int { return 768 }

func (p *Ollama) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	body := map[string]interface{}{
		"model": p.model,
		"input": texts,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/embed", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embed: HTTP %d", resp.StatusCode)
	}

	var r struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}

	return r.Embeddings, nil
}
