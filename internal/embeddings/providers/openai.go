package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type OpenAI struct {
	apiKey  string
	baseURL string
	client  *http.Client
	model   string
	dim     int
}

// NewOpenAI creates an OpenAI-compatible embedding provider. Empty baseURL
// falls back to the official API; empty model falls back to the default.
func NewOpenAI(apiKey, baseURL, model string) *OpenAI {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = defaultOpenAIEmbeddingModel
	}
	return &OpenAI{apiKey: apiKey, baseURL: strings.TrimRight(baseURL, "/"), client: &http.Client{}, model: model, dim: openAIEmbeddingDim(model)}
}

const defaultOpenAIEmbeddingModel = "text-embedding-3-small"

// openAIEmbeddingDim returns the vector dimension for the common OpenAI
// embedding models; unknown models keep the small-model default.
func openAIEmbeddingDim(model string) int {
	switch model {
	case "text-embedding-3-large":
		return 3072
	default:
		return 1536
	}
}

func (p *OpenAI) Name() string    { return "openai" }
func (p *OpenAI) Dimensions() int { return p.dim }

func (p *OpenAI) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	body := map[string]interface{}{
		"model": p.model,
		"input": texts,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/embeddings", bytes.NewReader(jsonBody))
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai embeddings: status %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	vecs := make([][]float32, len(result.Data))
	for i, d := range result.Data {
		vecs[i] = d.Embedding
	}
	return vecs, nil
}
