// Codegraph extension for Mneme.
//
// Git-aware code indexing with lexical search. Uses git ls-files to discover
// tracked files and indexes content for word-boundary/substring search.
//
// Tools: codegraph_index, codegraph_search, codegraph_status, codegraph_list_files
// Communicates via stdin/stdout JSON-RPC 2.0.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/simon/mneme/pkg/tools"
)

// ── Extension protocol (lightweight; full types from pkg/tools) ─────────────

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type callToolParams struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}
type callToolResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

// ── Manifest ─────────────────────────────────────────────────────────────────

type manifest struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
	ProtocolMin int      `json:"protocol_min"`
}

var extManifest = manifest{
	Name:        "codegraph",
	Version:     "0.2.0",
	Description: "Code indexing and search: git-aware file listing, content indexing, and lexical code search",
	Tools:       []string{"codegraph_index", "codegraph_search", "codegraph_status", "codegraph_list_files"},
	ProtocolMin: 1,
}

type toolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Permission  string                 `json:"permission"`
	HasEffects  bool                   `json:"has_effects"`
}

var toolDefs = []toolDef{
	{
		Name:        "codegraph_index",
		Description: "Index a git repository for code search. Call this before codegraph_search on a new repository.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Absolute path to the git repository to index. Defaults to MNEME_WORKSPACE.",
				},
				"ref": map[string]interface{}{
					"type":        "string",
					"description": "Git ref to index (optional, defaults to HEAD).",
				},
			},
		},
		Permission: tools.PermReadOnly.String(),
		HasEffects: false,
	},
	{
		Name:        "codegraph_search",
		Description: "Search code in indexed repositories using lexical matching. Auto-indexes if not yet indexed. Returns ranked results with line numbers and context.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query. Supports word-boundary and substring matching.",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Absolute path to the git repository. Defaults to MNEME_WORKSPACE.",
				},
				"k": map[string]interface{}{
					"type":        "integer",
					"description": "Max results (default 10, max 50).",
				},
			},
			"required": []string{"query"},
		},
		Permission: tools.PermReadOnly.String(),
		HasEffects: false,
	},
	{
		Name:        "codegraph_status",
		Description: "Get indexing status and statistics for a repository.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Absolute path to the git repository. Defaults to MNEME_WORKSPACE.",
				},
			},
		},
		Permission: tools.PermReadOnly.String(),
		HasEffects: false,
	},
	{
		Name:        "codegraph_list_files",
		Description: "List files tracked by git in a repository.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Absolute path to the git repository. Defaults to MNEME_WORKSPACE.",
				},
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "Optional glob pattern to filter files (e.g. '*.go').",
				},
			},
		},
		Permission: tools.PermReadOnly.String(),
		HasEffects: false,
	},
}

// ── Index ────────────────────────────────────────────────────────────────────

type indexedFile struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Lines   int    `json:"lines"`
	Content string `json:"-"`
}

type repoIndex struct {
	RepoPath   string        `json:"repo_path"`
	Ref        string        `json:"ref"`
	Files      []indexedFile `json:"files"`
	FileCount  int           `json:"file_count"`
	TotalSize  int64         `json:"total_size"`
	TotalLines int           `json:"total_lines"`
	IndexedAt  time.Time     `json:"indexed_at"`
}

var indexes = map[string]*repoIndex{}

func workspaceDir() string {
	if d := os.Getenv("MNEME_HOME"); d != "" {
		return d
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "data")
	}
	return os.TempDir()
}

// ── Main ─────────────────────────────────────────────────────────────────────

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("codegraph extension starting")
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return
			}
			return
		}
		var req rpcRequest
		json.Unmarshal(line, &req)
		resp := handleRequest(&req)
		respBytes, _ := json.Marshal(resp)
		fmt.Fprintf(os.Stdout, "%s\n", respBytes)
	}
}

func handleRequest(req *rpcRequest) *rpcResponse {
	switch req.Method {
	case "extension.describe":
		result, _ := json.Marshal(extManifest)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.list_tools":
		result, _ := json.Marshal(map[string]interface{}{"tools": toolDefs})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.list_agents":
		result, _ := json.Marshal(map[string]interface{}{"agents": []interface{}{}})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.call_tool":
		var params callToolParams
		json.Unmarshal(req.Params, &params)
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		var result callToolResult
		switch params.Name {
		case "codegraph_index":
			result = doIndex(ctx, params.Args)
		case "codegraph_search":
			result = doSearch(ctx, params.Args)
		case "codegraph_status":
			result = doStatus(params.Args)
		case "codegraph_list_files":
			result = doListFiles(ctx, params.Args)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown tool: %s", params.Name)}
		}
		res, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: res}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID,
			Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown method: %s", req.Method)}}
	}
}

// ── Tool implementations ───────────────────────────────────────────────────

func repoPath(args map[string]interface{}) string {
	if p, _ := args["path"].(string); p != "" {
		return p
	}
	return workspaceDir()
}

func doIndex(ctx context.Context, args map[string]interface{}) callToolResult {
	rp := repoPath(args)
	ref, _ := args["ref"].(string)

	files, err := gitLSFiles(ctx, rp, ref)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("git ls-files: %v", err)}
	}
	if len(files) == 0 {
		return callToolResult{Output: fmt.Sprintf("No git-tracked files found in %s.", rp)}
	}

	entries := make([]indexedFile, 0, len(files))
	var totalSize int64
	var totalLines int
	for _, path := range files {
		fullPath := filepath.Join(rp, path)
		info, err := os.Stat(fullPath)
		if err != nil || info.IsDir() || info.Size() > 1<<20 {
			continue
		}
		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		content := string(data)
		lines := strings.Count(content, "\n") + 1
		entries = append(entries, indexedFile{
			Path: path, Size: info.Size(), Lines: lines, Content: content,
		})
		totalSize += info.Size()
		totalLines += lines
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	idx := &repoIndex{
		RepoPath: rp, Ref: ref, Files: entries,
		FileCount: len(entries), TotalSize: totalSize, TotalLines: totalLines,
		IndexedAt: time.Now(),
	}
	if ref == "" {
		idx.Ref = currentRef(rp)
	}
	indexes[rp] = idx

	out, _ := json.Marshal(map[string]interface{}{
		"repo": rp, "ref": idx.Ref,
		"files": idx.FileCount, "size_mb": float64(totalSize) / (1 << 20),
		"lines": totalLines, "indexed_at": idx.IndexedAt.Format(time.RFC3339),
	})
	return callToolResult{Success: true, Output: string(out)}
}

func doSearch(ctx context.Context, args map[string]interface{}) callToolResult {
	rp := repoPath(args)
	query, _ := args["query"].(string)
	if query == "" {
		return callToolResult{Error: "query is required"}
	}
	k := 10
	if v, ok := args["k"].(float64); ok && v > 0 {
		k = int(v)
		if k > 50 {
			k = 50
		}
	}

	// Auto-index if not yet indexed.
	if _, ok := indexes[rp]; !ok {
		if res := doIndex(ctx, map[string]interface{}{"path": rp}); res.Error != "" {
			return res
		}
	}
	idx := indexes[rp]
	if idx == nil || len(idx.Files) == 0 {
		return callToolResult{Error: fmt.Sprintf("No indexed files in %s. Run codegraph_index first.", rp)}
	}

	type hit struct {
		Path    string `json:"path"`
		Line    int    `json:"line"`
		Content string `json:"content"`
		Score   int    `json:"score"`
	}
	var hits []hit
	queryLower := strings.ToLower(query)

	for _, f := range idx.Files {
		if !strings.Contains(strings.ToLower(f.Content), queryLower) {
			continue
		}
		lines := strings.Split(f.Content, "\n")
		for i, line := range lines {
			if !strings.Contains(strings.ToLower(line), queryLower) {
				continue
			}
			score := 1
			for _, word := range strings.Fields(queryLower) {
				if strings.Contains(strings.ToLower(line), word) {
					score += 2
				}
			}
			ctx := strings.TrimSpace(line)
			if len(ctx) > 300 {
				ctx = ctx[:300] + "..."
			}
			hits = append(hits, hit{Path: f.Path, Line: i + 1, Content: ctx, Score: score})
		}
	}

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Path < hits[j].Path
	})

	coverage := "full"
	total := len(hits)
	if total > k {
		hits = hits[:k]
		coverage = "partial"
	}
	if total == 0 {
		return callToolResult{Output: fmt.Sprintf("No matches for '%s' in %d indexed files (%s).", query, len(idx.Files), rp)}
	}

	out, _ := json.Marshal(map[string]interface{}{
		"hits": hits, "total": total, "coverage": coverage,
		"indexed": len(idx.Files), "repo": rp,
	})
	result := string(out)
	if coverage != "full" {
		result += "\n[NOTE: showing top " + fmt.Sprint(k) + " of " + fmt.Sprint(total) + " results. Narrow your query or use grep for complete results.]"
	}
	return callToolResult{Success: true, Output: result}
}

func doStatus(args map[string]interface{}) callToolResult {
	rp := repoPath(args)
	idx := indexes[rp]
	if idx == nil {
		return callToolResult{Output: fmt.Sprintf("Not indexed: %s. Run codegraph_index.", rp)}
	}
	out, _ := json.Marshal(map[string]interface{}{
		"repo": idx.RepoPath, "ref": idx.Ref,
		"files": idx.FileCount, "size_mb": float64(idx.TotalSize) / (1 << 20),
		"lines": idx.TotalLines, "indexed_at": idx.IndexedAt.Format(time.RFC3339),
	})
	return callToolResult{Success: true, Output: string(out)}
}

func doListFiles(ctx context.Context, args map[string]interface{}) callToolResult {
	rp := repoPath(args)
	ref, _ := args["ref"].(string)
	pattern, _ := args["pattern"].(string)

	files, err := gitLSFiles(ctx, rp, ref)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("git ls-files: %v", err)}
	}
	var filtered []string
	for _, f := range files {
		if pattern == "" || matchPattern(f, pattern) {
			filtered = append(filtered, f)
		}
	}
	if len(filtered) > 200 {
		filtered = filtered[:200]
	}
	if len(filtered) == 0 {
		return callToolResult{Output: "No matching files."}
	}
	out, _ := json.Marshal(map[string]interface{}{"files": filtered, "total": len(filtered), "repo": rp})
	return callToolResult{Success: true, Output: string(out)}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func gitLSFiles(ctx context.Context, dir, ref string) ([]string, error) {
	args := []string{"ls-files", "--cached", "--others", "--exclude-standard"}
	if ref != "" {
		args = []string{"ls-tree", "-r", "--name-only", ref}
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, string(out))
	}
	var files []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}

func currentRef(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "HEAD"
	}
	return strings.TrimSpace(string(out))
}

func matchPattern(path, pattern string) bool {
	matched, err := filepath.Match(pattern, filepath.Base(path))
	if err != nil {
		return strings.Contains(strings.ToLower(path), strings.ToLower(pattern))
	}
	return matched
}
