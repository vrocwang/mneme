// Package testutil provides test helpers: mock factories, fixture loading,
// and assertion utilities for both internal unit tests and extension tests.
package testutil

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TempDir creates a temporary directory for testing and returns a cleanup function.
func TempDir(t testing.TB) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "oh-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	return dir, func() { os.RemoveAll(dir) }
}

// WriteFile creates a file with content in a test directory.
func WriteFile(t testing.TB, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("WriteFile mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// FixturePath returns the absolute path to a test fixture file.
func FixturePath(relativePath string) string {
	return filepath.Join("testdata", relativePath)
}

// ReadFixture reads a test fixture file.
func ReadFixture(t testing.TB, relativePath string) string {
	t.Helper()
	data, err := os.ReadFile(FixturePath(relativePath))
	if err != nil {
		t.Fatalf("ReadFixture %q: %v", relativePath, err)
	}
	return string(data)
}

// ContextWithTimeout creates a context with a short timeout for tests.
func ContextWithTimeout(t testing.TB) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 5*time.Second)
}

// AssertNoError fails the test if err is not nil.
func AssertNoError(t testing.TB, err error, msg ...string) {
	t.Helper()
	if err != nil {
		if len(msg) > 0 {
			t.Fatalf("%s: %v", msg[0], err)
		}
		t.Fatalf("unexpected error: %v", err)
	}
}

// AssertEqual fails if a != b.
func AssertEqual(t testing.TB, a, b interface{}, msg ...string) {
	t.Helper()
	if a != b {
		if len(msg) > 0 {
			t.Fatalf("%s: expected %v, got %v", msg[0], b, a)
		}
		t.Fatalf("expected %v, got %v", b, a)
	}
}

// AssertTrue fails if cond is false.
func AssertTrue(t testing.TB, cond bool, msg ...string) {
	t.Helper()
	if !cond {
		if len(msg) > 0 {
			t.Fatalf("expected true: %s", msg[0])
		}
		t.Fatal("expected true")
	}
}

// MockClock returns a fixed time for deterministic tests.
type MockClock struct{ Now time.Time }

func (c *MockClock) Time() time.Time      { return c.Now }
func NewMockClock(t time.Time) *MockClock { return &MockClock{Now: t} }
