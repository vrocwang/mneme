// Codegraph extension for Mneme.
//
// Git-aware code indexing with lexical search. Uses git ls-files to discover
// tracked files and indexes content for word-boundary/substring search.
//
// Tools: codegraph_index, codegraph_search, codegraph_status, codegraph_list_files
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/simon/mneme/pkg/extsdk"
)

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
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "codegraph",
		Version:     "0.2.0",
		Description: "Code indexing and search: git-aware file listing, content indexing, and lexical code search",
	})

	srv.RegisterTool(extsdk.ToolDef{
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
		Permission: "read_only",
		HasEffects: false,
	}, doIndex)

	srv.RegisterTool(extsdk.ToolDef{
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
		Permission: "read_only",
		HasEffects: false,
	}, doSearch)

	srv.RegisterTool(extsdk.ToolDef{
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
		Permission: "read_only",
		HasEffects: false,
	}, doStatus)

	srv.RegisterTool(extsdk.ToolDef{
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
		Permission: "read_only",
		HasEffects: false,
	}, doListFiles)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "codegraph: %v\n", err)
		os.Exit(1)
	}
}

// ── Tool implementations ───────────────────────────────────────────────────

func repoPath(args map[string]interface{}) string {
	if p, _ := args["path"].(string); p != "" {
		return p
	}
	return workspaceDir()
}

func doIndex(ctx context.Context, args map[string]interface{}) extsdk.Result {
	rp := repoPath(args)
	ref, _ := args["ref"].(string)

	files, err := gitLSFiles(ctx, rp, ref)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("git ls-files: %v", err)}
	}
	if len(files) == 0 {
		return extsdk.Result{Output: fmt.Sprintf("No git-tracked files found in %s.", rp)}
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
	return extsdk.Result{Success: true, Output: string(out)}
}

func doSearch(ctx context.Context, args map[string]interface{}) extsdk.Result {
	rp := repoPath(args)
	query, _ := args["query"].(string)
	if query == "" {
		return extsdk.Result{Error: "query is required"}
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
		return extsdk.Result{Error: fmt.Sprintf("No indexed files in %s. Run codegraph_index first.", rp)}
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
		return extsdk.Result{Output: fmt.Sprintf("No matches for '%s' in %d indexed files (%s).", query, len(idx.Files), rp)}
	}

	out, _ := json.Marshal(map[string]interface{}{
		"hits": hits, "total": total, "coverage": coverage,
		"indexed": len(idx.Files), "repo": rp,
	})
	result := string(out)
	if coverage != "full" {
		result += "\n[NOTE: showing top " + fmt.Sprint(k) + " of " + fmt.Sprint(total) + " results. Narrow your query or use grep for complete results.]"
	}
	return extsdk.Result{Success: true, Output: result}
}

func doStatus(ctx context.Context, args map[string]interface{}) extsdk.Result {
	_ = ctx
	rp := repoPath(args)
	idx := indexes[rp]
	if idx == nil {
		return extsdk.Result{Output: fmt.Sprintf("Not indexed: %s. Run codegraph_index.", rp)}
	}
	out, _ := json.Marshal(map[string]interface{}{
		"repo": idx.RepoPath, "ref": idx.Ref,
		"files": idx.FileCount, "size_mb": float64(idx.TotalSize) / (1 << 20),
		"lines": idx.TotalLines, "indexed_at": idx.IndexedAt.Format(time.RFC3339),
	})
	return extsdk.Result{Success: true, Output: string(out)}
}

func doListFiles(ctx context.Context, args map[string]interface{}) extsdk.Result {
	rp := repoPath(args)
	ref, _ := args["ref"].(string)
	pattern, _ := args["pattern"].(string)

	files, err := gitLSFiles(ctx, rp, ref)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("git ls-files: %v", err)}
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
		return extsdk.Result{Output: "No matching files."}
	}
	out, _ := json.Marshal(map[string]interface{}{"files": filtered, "total": len(filtered), "repo": rp})
	return extsdk.Result{Success: true, Output: string(out)}
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
