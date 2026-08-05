package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaEmbedderName(t *testing.T) {
	o := NewOllama("http://localhost:11434")
	if o.Name() != "ollama" {
		t.Errorf("expected name 'ollama', got %q", o.Name())
	}
}

func TestOllamaEmbedderDimensions(t *testing.T) {
	o := NewOllama("http://localhost:11434")
	if o.Dimensions() != 768 {
		t.Errorf("expected 768 dimensions, got %d", o.Dimensions())
	}
}

func TestOllamaEmbedSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		resp := map[string]interface{}{
			"embeddings": [][]float32{{0.1, 0.2, 0.3, 0.4, 0.5}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	o := NewOllama(srv.URL)
	vecs, err := o.Embed(context.Background(), []string{"hello world"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 1 {
		t.Fatalf("expected 1 vector, got %d", len(vecs))
	}
	if len(vecs[0]) != 5 {
		t.Errorf("expected 5 dims, got %d", len(vecs[0]))
	}
}

func TestOllamaEmbedServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	o := NewOllama(srv.URL)
	_, err := o.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestOllamaEmbedContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until context is cancelled.
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	o := NewOllama(srv.URL)
	_, err := o.Embed(ctx, []string{"test"})
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}
