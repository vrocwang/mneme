package approval

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// toolNameLiteral extracts the tool-name literal from registrations such as
// `Schema{Name: "read_file", ...}` / `ToolDescriptor{Name: "shell"}`.
var toolNameLiteral = regexp.MustCompile(`Name:\s*"([A-Za-z0-9_./-]+)"`)

// scanToolNameLiterals walks the repository's Go sources (internal/** and
// extensions/**) and returns every `Name: "..."` literal. Paths are resolved
// relative to this package's directory, which is the working directory of the
// test binary.
func scanToolNameLiterals(t *testing.T) map[string]string {
	t.Helper()
	repoRoot := filepath.Join("..", "..")
	corpus := make(map[string]string)

	for _, dir := range []string{
		filepath.Join(repoRoot, "internal"),
		filepath.Join(repoRoot, "extensions"),
	} {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if info.Name() == "node_modules" || info.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range toolNameLiteral.FindAllSubmatch(data, -1) {
				name := string(m[1])
				if _, seen := corpus[name]; !seen {
					corpus[name] = path
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scanning %s: %v", dir, err)
		}
	}
	return corpus
}

// TestApprovalHeuristicsMatchRealToolNames is the drift guard for the fallback
// risk heuristics in gate.go.
//
// Rule: every entry in the heuristic lists must either be an exact tool name
// found in the repository's Go sources, or a strict prefix of one (the lists are
// applied with prefix matching, so stems such as "read_" or "todo_" are
// intentional). An entry that matches neither is a reference to a tool that was
// renamed or deleted, which silently downgrades the heuristic to "execute".
func TestApprovalHeuristicsMatchRealToolNames(t *testing.T) {
	corpus := scanToolNameLiterals(t)

	// Sanity check: a scan that silently found nothing must not make this test
	// pass vacuously.
	if len(corpus) < 100 {
		t.Fatalf("tool-name corpus is implausibly small (%d entries); the source scan is broken", len(corpus))
	}

	lists := []struct {
		name    string
		entries []string
	}{
		{"safeToolPrefixes", safeToolPrefixes},
		{"writeToolPrefixes", writeToolPrefixes},
		{"sideEffectToolPrefixes", sideEffectToolPrefixes},
	}
	for _, l := range lists {
		if len(l.entries) == 0 {
			t.Errorf("%s is empty", l.name)
		}
		for _, entry := range l.entries {
			if toolEntryExists(corpus, entry) {
				continue
			}
			t.Errorf("%s entry %q matches no tool name in internal/** or extensions/**; "+
				"remove it or update it to the renamed tool", l.name, entry)
		}
	}

	if len(executeToolNames) == 0 {
		t.Error("executeToolNames is empty")
	}
	for name := range executeToolNames {
		if _, ok := corpus[name]; !ok {
			t.Errorf("executeToolNames entry %q is an exact-match tool name that no longer exists", name)
		}
	}
}

// toolEntryExists reports whether an entry is an exact tool name or a strict
// prefix of one.
func toolEntryExists(corpus map[string]string, entry string) bool {
	if _, ok := corpus[entry]; ok {
		return true
	}
	for name := range corpus {
		if strings.HasPrefix(name, entry) {
			return true
		}
	}
	return false
}

// TestGuessToolRiskHeuristic guards the heuristic's behaviour after the lists
// were extracted (and the stale image_info/browser_open/shell_cmd entries
// removed). The stale names must now fall through to the conservative level.
func TestGuessToolRiskHeuristic(t *testing.T) {
	tests := []struct {
		tool string
		want int
	}{
		{"read_file", 1},
		{"list_dir", 1},
		{"memory_search", 1},
		{"todo_list", 1},
		{"write_file", 2},
		{"edit_file", 2},
		{"todo_update_status", 2},
		{"csv_export", 2},
		{"shell", 3},
		{"run_tests", 3},
		{"browser", 3},
		{"http_post", 3},
		{"mcp_connect", 4},
		{"mcp_", 3},
		{"unknown_tool_xyz", 3},
		// Removed stale names: no longer recognised, so they take the
		// conservative default instead of being treated as read-only/execute.
		{"image_info", 3},
		{"browser_open", 3},
		{"shell_cmd", 3},
	}
	for _, tt := range tests {
		if got := guessToolRiskHeuristic(tt.tool); got != tt.want {
			t.Errorf("guessToolRiskHeuristic(%q) = %d, want %d", tt.tool, got, tt.want)
		}
	}
}

// TestToolHasSideEffectsHeuristic covers the side-effect list used when no
// registry resolver is wired.
func TestToolHasSideEffectsHeuristic(t *testing.T) {
	var g Gate // no resolveTool -> heuristic path

	sideEffects := []string{"write_file", "edit_file", "shell", "http_post", "git", "mcp_execute", "memory_save"}
	for _, tool := range sideEffects {
		if !g.toolHasSideEffects(tool) {
			t.Errorf("toolHasSideEffects(%q) = false, want true", tool)
		}
	}
	readOnly := []string{"read_file", "web_search", "memory_search", "current_time"}
	for _, tool := range readOnly {
		if g.toolHasSideEffects(tool) {
			t.Errorf("toolHasSideEffects(%q) = true, want false", tool)
		}
	}
}
