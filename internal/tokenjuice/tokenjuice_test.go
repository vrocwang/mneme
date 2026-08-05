package tokenjuice

import "testing"

func TestCompact_SmallEnough(t *testing.T) {
	input := "hello world"
	result := Compact(input, 100)
	if result != input {
		t.Errorf("small text should not change: got %q", result)
	}
}

func TestCompact_TruncatesLarge(t *testing.T) {
	input := ""
	for i := 0; i < 10000; i++ {
		input += "line of text that goes on and on\n"
	}

	result := Compact(input, 100)
	if len(result) >= len(input) {
		t.Error("result should be shorter than input")
	}
	if !containsText(result, "[truncated") {
		t.Error("result should contain truncation marker")
	}
}

func TestCompact_PreservesHead(t *testing.T) {
	input := "BEGIN IMPORTANT DATA\n" + repeated("filler\n", 5000) + "END IMPORTANT DATA\n"

	result := Compact(input, 100)
	if !containsText(result, "BEGIN IMPORTANT DATA") {
		t.Error("should preserve head")
	}
	if !containsText(result, "END IMPORTANT DATA") {
		t.Error("should preserve tail")
	}
}

func repeated(s string, n int) string {
	var result string
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}

func containsText(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
