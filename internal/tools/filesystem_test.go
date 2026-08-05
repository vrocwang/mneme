package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadFile_Success(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello world"), 0644)

	tool := NewReadFile(dir)
	result := tool.Execute(context.Background(), map[string]interface{}{
		"path": "test.txt",
	})

	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Output != "hello world" {
		t.Errorf("expected 'hello world', got %q", result.Output)
	}
}

func TestReadFile_NotFound(t *testing.T) {
	tool := NewReadFile(t.TempDir())
	result := tool.Execute(context.Background(), map[string]interface{}{
		"path": "nonexistent.txt",
	})
	if result.Success {
		t.Error("expected failure for missing file")
	}
}

func TestWriteFile_Success(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteFile(dir)
	result := tool.Execute(context.Background(), map[string]interface{}{
		"path":    "subdir/new.txt",
		"content": "created content",
	})

	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "subdir", "new.txt"))
	if string(data) != "created content" {
		t.Errorf("expected 'created content', got %q", string(data))
	}
}

func TestWriteFile_NoPath(t *testing.T) {
	tool := NewWriteFile(t.TempDir())
	result := tool.Execute(context.Background(), map[string]interface{}{})
	if result.Success {
		t.Error("expected failure without path")
	}
}

func TestListDir_Success(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte{}, 0644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0755)

	tool := NewListDir(dir)
	result := tool.Execute(context.Background(), map[string]interface{}{
		"path": ".",
	})

	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if !containsAny(result.Output, "a.txt", "sub") {
		t.Errorf("output should contain filenames: %s", result.Output)
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if !containsStr(s, sub) {
			return false
		}
	}
	return true
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
