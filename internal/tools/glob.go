package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/simon/mneme/internal/security"
)

const maxGlobResults = 500

// Glob finds files matching a pattern in the workspace.
type Glob struct {
	workspaceRoot string
}

func NewGlob(workspaceRoot string) *Glob {
	return &Glob{workspaceRoot: workspaceRoot}
}

func (t *Glob) Schema() Schema {
	return Schema{
		Name:        "glob",
		Description: "Finds files matching a glob pattern within the workspace. Returns sorted relative file paths.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "Glob pattern (e.g. '**/*.go', 'src/**/*.tsx', '*.md').",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Optional subdirectory to search within (default: whole workspace).",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

func (t *Glob) PermissionLevel() PermissionLevel { return PermReadOnly }
func (t *Glob) SideEffects() bool                { return false }

func (t *Glob) Execute(ctx context.Context, args map[string]interface{}) Result {
	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return Result{Success: false, Error: "pattern is required"}
	}

	root := t.workspaceRoot
	if p, ok := args["path"].(string); ok && p != "" {
		joined := filepath.Join(t.workspaceRoot, p)
		resolvedPath, err := security.ValidatePath(joined, t.workspaceRoot)
		if err != nil {
			return Result{Success: false, Error: fmt.Sprintf("path blocked by security policy: %v", err)}
		}
		root = resolvedPath
	}

	fullPattern := filepath.Join(root, pattern)

	matches, err := filepath.Glob(fullPattern)
	if err != nil {
		return Result{Success: false, Error: fmt.Sprintf("glob error: %v", err)}
	}

	// Handle ** patterns (filepath.Glob doesn't support **)
	if strings.Contains(pattern, "**") {
		walkMatches, walkErr := walkGlob(root, pattern)
		if walkErr == nil {
			matches = append(matches, walkMatches...)
		}
	}

	// Deduplicate and sort
	seen := make(map[string]bool)
	var results []string
	for _, m := range matches {
		rel, err := filepath.Rel(t.workspaceRoot, m)
		if err != nil {
			continue
		}
		// Skip hidden files and dirs
		if strings.HasPrefix(filepath.Base(rel), ".") {
			continue
		}
		if seen[rel] {
			continue
		}
		seen[rel] = true

		info, err := os.Stat(m)
		if err != nil {
			continue
		}
		if info.IsDir() {
			rel += "/"
		}

		results = append(results, rel)

		if len(results) >= maxGlobResults {
			break
		}
	}

	sort.Strings(results)

	if len(results) == 0 {
		return Result{Success: true, Output: "No files found."}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d file(s):\n", len(results)))
	for _, r := range results {
		b.WriteString(r + "\n")
	}
	if len(results) >= maxGlobResults {
		b.WriteString(fmt.Sprintf("... (results truncated at %d)", maxGlobResults))
	}

	return Result{Success: true, Output: b.String()}
}

// walkGlob implements recursive ** matching. Since filepath.Match does not
// support **, we convert the glob pattern to a suffix after the last **
// and match against that.
func walkGlob(root, pattern string) ([]string, error) {
	var results []string

	// Extract the literal suffix after the last ** for matching.
	// e.g. "**/*.go" → suffix = "*.go", "src/**/*_test.go" → suffix = "*_test.go"
	suffix := pattern
	if idx := strings.LastIndex(pattern, "**"); idx >= 0 {
		suffix = pattern[idx+2:]
		suffix = strings.TrimPrefix(suffix, string(filepath.Separator))
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") && base != "." {
				return filepath.SkipDir
			}
			if base == "node_modules" || base == "vendor" || base == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		// Match against the suffix pattern after **.
		matched, _ := filepath.Match(suffix, filepath.Base(rel))
		if !matched && suffix != "" {
			// Also try matching the full relative path against the original
			// pattern for patterns like "src/**/*.go" where we need to check prefix.
			matched = matchGlobPattern(pattern, rel)
		}
		if matched || suffix == "" {
			abs, _ := filepath.Abs(path)
			results = append(results, abs)
		}

		if len(results) >= maxGlobResults {
			return filepath.SkipAll
		}
		return nil
	})

	return results, err
}

// matchGlobPattern handles ** in glob patterns by splitting on ** and
// checking each segment sequentially against the path.
func matchGlobPattern(pattern, path string) bool {
	parts := strings.Split(pattern, "**")
	remainder := path
	for i, part := range parts {
		if part == "" {
			continue
		}
		// Trim leading separator for all but the first segment.
		if i > 0 {
			part = strings.TrimPrefix(part, string(filepath.Separator))
		}
		if part == "" {
			continue
		}
		idx := findGlobSegment(remainder, part)
		if idx < 0 {
			return false
		}
		remainder = remainder[idx+len(part):]
	}
	return true
}

// findGlobSegment finds the position in path where the glob segment matches.
func findGlobSegment(path, segment string) int {
	// For simple patterns like "*.go", match against the last path component.
	if !strings.Contains(segment, string(filepath.Separator)) {
		base := filepath.Base(path)
		if matched, _ := filepath.Match(segment, base); matched {
			return len(path) - len(base)
		}
		// Also try matching at any path component boundary.
		parts := strings.Split(path, string(filepath.Separator))
		for i := len(parts) - 1; i >= 0; i-- {
			if matched, _ := filepath.Match(segment, parts[i]); matched {
				// Calculate byte offset of this component.
				off := 0
				for j := 0; j < i; j++ {
					off += len(parts[j]) + 1
				}
				return off
			}
		}
		return -1
	}
	// For multi-segment patterns like "subdir/*.go", check at each position.
	for i := 0; i <= len(path); i++ {
		end := i + len(segment)
		if end > len(path) {
			break
		}
		sub := path[i:end]
		// Only match at path component boundaries.
		if i > 0 && path[i-1] != filepath.Separator {
			continue
		}
		if end < len(path) && path[end] != filepath.Separator {
			continue
		}
		if matched, _ := filepath.Match(segment, sub); matched {
			return i
		}
	}
	return -1
}
