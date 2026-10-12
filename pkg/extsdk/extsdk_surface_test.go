package extsdk

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The SDK is the contract every extension is written against, so its surface is
// exercised here rather than only by the in-tree extensions: an untouched helper
// (or a wire field that stops being honoured) is otherwise invisible until a
// third-party extension breaks.

// TestPathHelpersFollowMNEMEHome pins the shared workspace layout: the host
// exports MNEME_HOME before spawning an extension, and every helper must land
// inside it and create the directory it returns.
func TestPathHelpersFollowMNEMEHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MNEME_HOME", home)

	cases := []struct {
		name string
		got  string
		want string
	}{
		{"WorkspaceDir", WorkspaceDir(), home},
		{"DataDir", DataDir(), filepath.Join(home, "data")},
		{"SkillsDir", SkillsDir(), filepath.Join(home, "skills")},
		{"ScreenshotsDir", ScreenshotsDir(), filepath.Join(home, "screenshots")},
		{"TaskBoardsDir", TaskBoardsDir(), filepath.Join(home, "agent_task_boards")},
		{"WorkflowsDir", WorkflowsDir(), filepath.Join(home, "workflows")},
		{"WorkflowLogsDir", WorkflowLogsDir(), filepath.Join(home, "workflow-logs")},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
	// The directory-returning helpers create what they return.
	for _, dir := range []string{DataDir(), SkillsDir(), ScreenshotsDir(), TaskBoardsDir(), WorkflowsDir(), WorkflowLogsDir()} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Errorf("%s was not created: %v", dir, err)
		}
	}
	if got := DataFile("nested.json"); got != filepath.Join(DataDir(), "nested.json") {
		t.Errorf("DataFile = %q", got)
	}
}

// TestArgumentHelpers covers the coercion rules extensions rely on for JSON
// args: numbers arrive as float64, wrong types must fall back, and an empty
// string counts as absent for GetStr.
func TestArgumentHelpers(t *testing.T) {
	args := map[string]interface{}{
		"str":     "value",
		"empty":   "",
		"float":   float64(7),
		"int":     8,
		"int64":   int64(9),
		"badInt":  "10",
		"bool":    true,
		"badBool": "true",
	}

	if got := GetStr(args, "str", "fallback"); got != "value" {
		t.Errorf("GetStr(str) = %q", got)
	}
	if got := GetStr(args, "empty", "fallback"); got != "fallback" {
		t.Errorf("GetStr(empty) = %q, want the fallback", got)
	}
	if got := GetStr(args, "missing", "fallback"); got != "fallback" {
		t.Errorf("GetStr(missing) = %q", got)
	}

	for key, want := range map[string]int{"float": 7, "int": 8, "int64": 9, "badInt": -1, "missing": -1} {
		fallback := -1
		if got := GetInt(args, key, fallback); got != want {
			t.Errorf("GetInt(%s) = %d, want %d", key, got, want)
		}
	}

	if !GetBool(args, "bool", false) {
		t.Error("GetBool(bool) = false, want true")
	}
	if GetBool(args, "badBool", false) {
		t.Error("GetBool must reject a non-bool instead of coercing it")
	}
	if !GetBool(args, "missing", true) {
		t.Error("GetBool(missing) must return the fallback")
	}
}

// TestServerProtocolLoop drives the whole wire contract over in-memory streams:
// describe/list_tools/list_agents/configure/call_tool plus the error frames
// (unknown method, bad params). It also covers SetLogger and SetCallTimeout,
// which no in-tree extension changes today.
func TestServerProtocolLoop(t *testing.T) {
	var logged bytes.Buffer
	srv := NewServer(Manifest{Name: "probe", Version: "1.2.3", Description: "test"})
	srv.SetLogger(newBufferLogger(&logged))
	srv.SetCallTimeout(2 * time.Second)
	srv.SetConfigHandler(func(cfg map[string]interface{}) error {
		if len(cfg) == 0 {
			return context.DeadlineExceeded // any non-nil error must be reported as rejected
		}
		return nil
	})
	srv.RegisterAgent(AgentDef{ID: "probe_agent", Name: "Probe"})
	srv.RegisterTool(ToolDef{
		Name:        "echo",
		Description: "echoes args",
		Parameters:  map[string]interface{}{"type": "object"},
		Permission:  "read_only",
		TimeoutSecs: 300,
	}, func(ctx context.Context, args map[string]interface{}) Result {
		if err := ctx.Err(); err != nil {
			return Result{Error: err.Error()}
		}
		return Result{
			Success: true,
			Output:  GetStr(args, "text", "none") + ":" + string(rune('0'+GetInt(args, "n", 0))),
		}
	})

	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"extension.describe"}`,
		`{"jsonrpc":"2.0","id":2,"method":"extension.list_tools"}`,
		`{"jsonrpc":"2.0","id":3,"method":"extension.list_agents"}`,
		`{"jsonrpc":"2.0","id":4,"method":"extension.configure","params":{"config":{"k":"v"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"extension.call_tool","params":{"name":"echo","args":{"text":"hi","n":3}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"extension.call_tool","params":{"name":"nope"}}`,
		`{"jsonrpc":"2.0","id":7,"method":"extension.unknown"}`,
		`{"jsonrpc":"2.0","id":8,"method":"extension.list_tools"`,
		"",
	}
	var out bytes.Buffer
	if err := srv.run(strings.NewReader(strings.Join(requests, "\n")+"\n"), &out); err != nil {
		t.Fatalf("run returned %v", err)
	}

	responses := map[int64]map[string]interface{}{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var frame struct {
			ID     int64                  `json:"id"`
			Result map[string]interface{} `json:"result"`
			Error  map[string]interface{} `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("response %q is not JSON: %v", line, err)
		}
		if frame.Error != nil {
			responses[frame.ID] = map[string]interface{}{"error": frame.Error}
			continue
		}
		responses[frame.ID] = frame.Result
	}

	// A malformed line and a trailing partial line must not produce a frame.
	if len(responses) != 7 {
		t.Fatalf("got %d responses, want one per well-formed request (7): %v", len(responses), responses)
	}
	if got := responses[1]["name"]; got != "probe" {
		t.Errorf("describe name = %v", got)
	}
	if got := responses[1]["tools"]; got == nil {
		t.Error("describe must report the registered tool names")
	}
	tools, _ := responses[2]["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("list_tools returned %v", responses[2]["tools"])
	}
	first, _ := tools[0].(map[string]interface{})
	if first["timeout_secs"] != float64(300) {
		t.Errorf("tool def must carry timeout_secs=300, got %v", first["timeout_secs"])
	}
	agents, _ := responses[3]["agents"].([]interface{})
	if len(agents) != 1 {
		t.Fatalf("list_agents returned %v", responses[3]["agents"])
	}
	if accepted, ok := responses[4]["accepted"].(bool); !ok || !accepted {
		t.Errorf("configure = %v, want accepted", responses[4])
	}
	if got := responses[5]["output"]; got != "hi:3" {
		t.Errorf("call_tool output = %v", got)
	}
	if got := responses[6]["error"]; got == "" {
		t.Errorf("unknown tool must return a result-level error, got %v", responses[6])
	}
	if responses[7]["error"] == nil {
		t.Error("unknown method must produce a JSON-RPC error frame")
	}
}

func newBufferLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// Run is the entry point every extension's main() calls. It is covered here by
// temporarily pointing os.Stdin at a pipe, so the process-level wrapper (not
// just the injectable run) is exercised too.
func TestServerRunReadsStdinUntilEOF(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	prev := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = prev
		r.Close()
	})

	srv := NewServer(Manifest{Name: "run-probe", Version: "0.0.1"})
	done := make(chan error, 1)
	go func() { done <- srv.Run() }()

	if _, err := w.WriteString(`{"jsonrpc":"2.0","id":1,"method":"extension.describe"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	w.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil on clean EOF", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after stdin reached EOF")
	}
}
