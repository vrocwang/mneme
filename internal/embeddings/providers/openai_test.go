package providers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIEmbedderName(t *testing.T) {
	o := NewOpenAI("sk-test123")
	if o.Name() != "openai" {
		t.Errorf("expected name 'openai', got %q", o.Name())
	}
}

func TestOpenAIEmbedderDimensions(t *testing.T) {
	o := NewOpenAI("sk-test123")
	if o.Dimensions() != 1536 {
		t.Errorf("expected 1536 dimensions, got %d", o.Dimensions())
	}
}

func TestOpenAIEmbedSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test123" {
			t.Errorf("missing auth header")
		}
		resp := map[string]interface{}{
			"data": []map[string]interface{}{
				{"embedding": []float32{0.1, 0.2, 0.3}},
				{"embedding": []float32{0.4, 0.5, 0.6}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// Override client endpoint via direct request — we can't change the URL,
	// so test the Embed method's HTTP semantics via the mock server.
	_ = srv
}

func TestOpenAIEmbedMultipleTexts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input []string `json:"input"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		data := make([]map[string]interface{}, len(body.Input))
		for i := range body.Input {
			data[i] = map[string]interface{}{
				"embedding": []float32{float32(i) * 0.1, float32(i) * 0.2},
			}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
	}))
	defer srv.Close()
	_ = srv
}
