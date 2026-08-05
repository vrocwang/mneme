package http_host

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	s := New(nil)
	if s == nil {
		t.Fatal("expected non-nil server")
	}
}

func TestServeDirAndStop(t *testing.T) {
	dir, err := os.MkdirTemp("", "oh-httphost-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	defer os.RemoveAll(dir)

	// Create a test file
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>Hello</h1>"), 0644)

	s := New(nil)
	ctx := context.Background()

	url, err := s.ServeDir(ctx, "test", dir)
	if err != nil {
		t.Fatalf("ServeDir: %v", err)
	}
	defer s.StopAll()

	if url == "" {
		t.Fatal("expected non-empty URL")
	}

	// Verify the server responds
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url + "/index.html")
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestList(t *testing.T) {
	dir, err := os.MkdirTemp("", "oh-httphost-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	defer os.RemoveAll(dir)

	s := New(nil)
	ctx := context.Background()

	url, err := s.ServeDir(ctx, "test1", dir)
	if err != nil {
		t.Fatalf("ServeDir: %v", err)
	}
	defer s.StopAll()

	list := s.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 server, got %d", len(list))
	}
	if list["test1"] != url {
		t.Fatalf("expected %s, got %s", url, list["test1"])
	}
}

func TestStopNonexistent(t *testing.T) {
	s := New(nil)
	err := s.Stop("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent key")
	}
}

func TestStopAll(t *testing.T) {
	dir, err := os.MkdirTemp("", "oh-httphost-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	defer os.RemoveAll(dir)

	s := New(nil)
	ctx := context.Background()

	s.ServeDir(ctx, "a", dir)
	s.ServeDir(ctx, "b", dir)
	s.StopAll()

	if len(s.List()) != 0 {
		t.Fatal("expected empty list after StopAll")
	}
}
