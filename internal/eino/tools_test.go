package eino

import (
	"testing"

	"github.com/simon/mneme/internal/tools"
)

func TestToResult_Success(t *testing.T) {
	out, err := toResult(tools.Result{Success: true, Output: "hello"})
	if err != nil {
		t.Fatalf("success should not produce an error: %v", err)
	}
	if out != "hello" {
		t.Errorf("expected output passthrough, got %q", out)
	}
}

func TestToResult_FailureWithOutput(t *testing.T) {
	// A failed result that still carries output returns the output as the
	// string and the error separately, so the model sees partial context.
	out, err := toResult(tools.Result{Success: false, Output: "partial", Error: "boom"})
	if err == nil {
		t.Fatal("failure should produce an error")
	}
	if out != "partial" {
		t.Errorf("expected partial output, got %q", out)
	}
	if err.Error() != "boom" {
		t.Errorf("expected error 'boom', got %q", err.Error())
	}
}

func TestToResult_FailureNoOutput(t *testing.T) {
	out, err := toResult(tools.Result{Success: false, Error: "boom"})
	if err == nil {
		t.Fatal("failure should produce an error")
	}
	if out != "" {
		t.Errorf("expected empty output, got %q", out)
	}
}
