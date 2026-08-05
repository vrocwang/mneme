package approval

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactArgs_SensitiveStringField(t *testing.T) {
	args := map[string]interface{}{
		"body":   "hello world",
		"action": "execute",
	}
	red := RedactArgs(args)

	if red["action"] != "execute" {
		t.Errorf("non-sensitive field 'action' should pass through, got %v", red["action"])
	}
	body, ok := red["body"].(string)
	if !ok || !strings.HasPrefix(body, "<redacted: string") {
		t.Errorf("sensitive field 'body' should be redacted, got %v", red["body"])
	}
}

func TestRedactArgs_NestedSensitiveFields(t *testing.T) {
	args := map[string]interface{}{
		"action": "execute",
		"params": map[string]interface{}{
			"message":    "secret",
			"channel_id": "C123",
			"tool_slug":  "SLACK_SEND",
		},
	}
	red := RedactArgs(args)

	params, ok := red["params"].(map[string]interface{})
	if !ok {
		t.Fatal("params should be a map")
	}
	if msg, ok := params["message"].(string); !ok || !strings.HasPrefix(msg, "<redacted: string") {
		t.Errorf("nested 'message' should be redacted, got %v", params["message"])
	}
	if params["channel_id"] != "C123" {
		t.Errorf("'channel_id' should pass through, got %v", params["channel_id"])
	}
}

func TestRedactArgs_CaseInsensitive(t *testing.T) {
	args := map[string]interface{}{
		"Body":  "x",
		"TOKEN": "y",
	}
	red := RedactArgs(args)

	if b, ok := red["Body"].(string); !ok || !strings.HasPrefix(b, "<redacted") {
		t.Errorf("'Body' should be redacted (case-insensitive), got %v", red["Body"])
	}
	if tok, ok := red["TOKEN"].(string); !ok || !strings.HasPrefix(tok, "<redacted") {
		t.Errorf("'TOKEN' should be redacted (case-insensitive), got %v", red["TOKEN"])
	}
}

func TestRedactArgs_ArrayRedactsToCount(t *testing.T) {
	args := map[string]interface{}{
		"recipients": []interface{}{"a@x", "b@y", "c@z"},
	}
	red := RedactArgs(args)
	got, ok := red["recipients"].(string)
	if !ok || got != "<redacted: array (3 items)>" {
		t.Errorf("array field should redact to count marker, got %v", red["recipients"])
	}
}

func TestRedactArgs_NumberAndBool(t *testing.T) {
	args := map[string]interface{}{
		"token":  float64(12345),
		"secret": true,
	}
	red := RedactArgs(args)
	if red["token"] != "<redacted: number>" {
		t.Errorf("number should redact, got %v", red["token"])
	}
	if red["secret"] != "<redacted: bool>" {
		t.Errorf("bool should redact, got %v", red["secret"])
	}
}

func TestScrubPaths_UnixHome(t *testing.T) {
	input := "/Users/oxoxdev/work/mneme"
	got := scrubPaths(input)
	if strings.Contains(got, "oxoxdev") {
		t.Errorf("username should be scrubbed, got %s", got)
	}
	if !strings.Contains(got, "<HOME>") {
		t.Errorf("should contain <HOME> marker, got %s", got)
	}
	if !strings.HasSuffix(got, "/work/mneme") {
		t.Errorf("path after username should be preserved, got %s", got)
	}
}

func TestScrubPaths_LinuxHome(t *testing.T) {
	input := "/home/jane/project"
	got := scrubPaths(input)
	if strings.Contains(got, "jane") {
		t.Errorf("username should be scrubbed, got %s", got)
	}
	if !strings.Contains(got, "<HOME>") {
		t.Errorf("should contain <HOME> marker, got %s", got)
	}
	if !strings.HasSuffix(got, "/project") {
		t.Errorf("path after username should be preserved, got %s", got)
	}
}

func TestScrubPaths_WindowsHome(t *testing.T) {
	input := `C:\Users\oxoxdev\work\mneme`
	got := scrubPaths(input)
	if strings.Contains(got, "oxoxdev") {
		t.Errorf("username should be scrubbed, got %s", got)
	}
	if !strings.Contains(got, "<HOME>") {
		t.Errorf("should contain <HOME> marker, got %s", got)
	}
	if !strings.HasSuffix(got, `\work\mneme`) {
		t.Errorf("path after username should be preserved, got %s", got)
	}
}

func TestScrubPaths_MultipleHomes(t *testing.T) {
	input := "from /Users/alice/a.txt to /Users/bob/b.txt"
	got := scrubPaths(input)
	if strings.Contains(got, "alice") || strings.Contains(got, "bob") {
		t.Errorf("all usernames should be scrubbed, got %s", got)
	}
	if strings.Count(got, "<HOME>") != 2 {
		t.Errorf("should have 2 <HOME> markers, got %s", got)
	}
}

func TestRedactArgsJSON(t *testing.T) {
	raw := `{"body":"secret message","action":"execute"}`
	red := RedactArgsJSON(raw)

	var m map[string]interface{}
	if err := json.Unmarshal([]byte(red), &m); err != nil {
		t.Fatalf("redacted output should be valid JSON: %v (got: %s)", err, red)
	}
	if m["action"] != "execute" {
		t.Errorf("'action' should pass through, got %v", m["action"])
	}
	if b, ok := m["body"].(string); !ok || !strings.HasPrefix(b, "<redacted") {
		t.Errorf("'body' should be redacted, got %v", m["body"])
	}
}

func TestRedactArgsJSON_InvalidInput(t *testing.T) {
	red := RedactArgsJSON("not json")
	if !strings.Contains(red, "unparseable") {
		t.Errorf("invalid JSON should produce safe fallback, got %s", red)
	}
}

func TestSummarizeAction(t *testing.T) {
	raw := `{"action":"execute","tool_slug":"SLACK_SEND","params":{"body":"hi"}}`
	summary := SummarizeAction("composio", raw)
	if !strings.Contains(summary, "composio") {
		t.Errorf("summary should contain tool name, got %s", summary)
	}
	if !strings.Contains(summary, "action=execute") {
		t.Errorf("summary should contain safe field action, got %s", summary)
	}
	if strings.Contains(summary, "hi") {
		t.Errorf("summary should NOT contain raw message body, got %s", summary)
	}
}

func TestSummarizeAction_EmptyArgs(t *testing.T) {
	summary := SummarizeAction("pushover", `{}`)
	if !strings.Contains(summary, "pushover") {
		t.Errorf("summary should contain tool name, got %s", summary)
	}
	if !strings.Contains(summary, "bytes") {
		t.Errorf("summary should mention bytes, got %s", summary)
	}
}
