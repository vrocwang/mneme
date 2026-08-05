package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/simon/mneme/internal/security"
)

const (
	maxGrepResults    = 200
	maxGrepFileSize   = 2 * 1024 * 1024 // 2MB per file
	regexMatchTimeout = 100 * time.Millisecond
)

// Grep searches for a pattern in files within the workspace.
type Grep struct {
	workspaceRoot string
}

func NewGrep(workspaceRoot string) *Grep {
	return &Grep{workspaceRoot: workspaceRoot}
}

func (t *Grep) Schema() Schema {
	return Schema{
		Name:        "grep",
		Description: "Searches for a pattern (text or regex) in files within the workspace. Returns matching lines with file paths and line numbers.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "Text or regex pattern to search for.",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Optional subdirectory to search within (default: whole workspace).",
				},
				"include": map[string]interface{}{
					"type":        "string",
					"description": "Optional file glob pattern to include (e.g. '*.go', '*.{ts,tsx}').",
				},
				"case_sensitive": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether the search is case-sensitive (default: false).",
				},
				"regex": map[string]interface{}{
					"type":        "boolean",
					"description": "Treat pattern as regex (default: false).",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

type grepMatch struct {
	file string
	line int
	text string
}

func (t *Grep) PermissionLevel() PermissionLevel { return PermReadOnly }
func (t *Grep) SideEffects() bool                { return false }

func (t *Grep) Execute(ctx context.Context, args map[string]interface{}) Result {
	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return Result{Success: false, Error: "pattern is required"}
	}

	searchPath := t.workspaceRoot
	if p, ok := args["path"].(string); ok && p != "" {
		joined := filepath.Join(t.workspaceRoot, p)
		resolvedPath, err := security.ValidatePath(joined, t.workspaceRoot)
		if err != nil {
			return Result{Success: false, Error: fmt.Sprintf("path blocked by security policy: %v", err)}
		}
		searchPath = resolvedPath
	}

	caseSensitive, _ := args["case_sensitive"].(bool)
	useRegex, _ := args["regex"].(bool)
	includeGlob, _ := args["include"].(string)

	var re *regexp.Regexp
	var err error
	if useRegex {
		expr := pattern
		if !caseSensitive {
			expr = "(?i)" + expr
		}
		re, err = regexp.Compile(expr)
		if err != nil {
			return Result{Success: false, Error: fmt.Sprintf("invalid regex: %v", err)}
		}
	}

	// Build the search string for non-regex mode
	searchStr := pattern
	if !caseSensitive {
		searchStr = strings.ToLower(searchStr)
	}

	var matches []grepMatch

	err = filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") && base != "." {
				return filepath.SkipDir
			}
			// Skip common non-source dirs
			if base == "node_modules" || base == "vendor" || base == ".git" || base == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}

		if info.Size() > maxGrepFileSize {
			return nil
		}

		// Glob filtering
		if includeGlob != "" {
			rel, _ := filepath.Rel(searchPath, path)
			matched, _ := filepath.Match(includeGlob, rel)
			if !matched {
				return nil
			}
		}

		func() {
			f, err := os.Open(path)
			if err != nil {
				return
			}
			defer f.Close()

			relPath, _ := filepath.Rel(t.workspaceRoot, path)

			scanner := bufio.NewScanner(f)
			scanner.Buffer(make([]byte, 64*1024), 1024*1024)
			lineNum := 0
			for scanner.Scan() {
				lineNum++
				if len(matches) >= maxGrepResults {
					return
				}

				line := scanner.Text()
				matched := false

				if useRegex {
					matched = matchWithTimeout(re, line, regexMatchTimeout)
				} else if caseSensitive {
					matched = strings.Contains(line, pattern)
				} else {
					matched = strings.Contains(strings.ToLower(line), searchStr)
				}

				if matched {
					matches = append(matches, grepMatch{file: relPath, line: lineNum, text: strings.TrimSpace(line)})
				}
			}
		}()
		if len(matches) >= maxGrepResults {
			return filepath.SkipAll
		}
		return nil
	})

	if err != nil && err != filepath.SkipAll {
		return Result{Success: false, Error: fmt.Sprintf("walk error: %v", err)}
	}

	if len(matches) == 0 {
		return Result{Success: true, Output: "No matches found."}
	}

	// Group by file
	var b strings.Builder
	currentFile := ""
	for _, m := range matches {
		if m.file != currentFile {
			if currentFile != "" {
				b.WriteString("\n")
			}
			b.WriteString(fmt.Sprintf("--- %s ---\n", m.file))
			currentFile = m.file
		}
		b.WriteString(fmt.Sprintf("%d: %s\n", m.line, m.text))
	}

	if len(matches) >= maxGrepResults {
		b.WriteString(fmt.Sprintf("\n... (results truncated at %d)", maxGrepResults))
	}

	return Result{Success: true, Output: b.String()}
}

// matchWithTimeout runs a regex match with a deadline to prevent ReDoS.
func matchWithTimeout(re *regexp.Regexp, s string, timeout time.Duration) bool {
	type result struct {
		matched bool
	}
	ch := make(chan result, 1)
	go func() {
		ch <- result{matched: re.MatchString(s)}
	}()
	select {
	case r := <-ch:
		return r.matched
	case <-time.After(timeout):
		return false
	}
}
