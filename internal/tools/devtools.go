package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/simon/mneme/internal/security"
)

// ── detect_tools ──────────────────────────────────────────────────────

type toolEntry struct {
	name     string
	category string
	args     []string // version detection args
}

var knownToolList = []toolEntry{
	// Version control
	{"git", "vcs", []string{"--version"}},
	// Languages & runtimes
	{"node", "language", []string{"--version"}},
	{"python3", "language", []string{"--version"}},
	{"python", "language", []string{"--version"}},
	{"go", "language", []string{"version"}},
	{"rustc", "language", []string{"--version"}},
	{"java", "language", []string{"-version"}},
	{"javac", "language", []string{"-version"}},
	{"swift", "language", []string{"--version"}},
	{"dotnet", "language", []string{"--version"}},
	{"ruby", "language", []string{"--version"}},
	{"perl", "language", []string{"--version"}},
	{"php", "language", []string{"--version"}},
	{"lua", "language", []string{"-v"}},
	{"zig", "language", []string{"version"}},
	// Package managers
	{"npm", "pkg", []string{"--version"}},
	{"pnpm", "pkg", []string{"--version"}},
	{"yarn", "pkg", []string{"--version"}},
	{"pip3", "pkg", []string{"--version"}},
	{"pip", "pkg", []string{"--version"}},
	{"cargo", "pkg", []string{"--version"}},
	{"gem", "pkg", []string{"--version"}},
	{"composer", "pkg", []string{"--version"}},
	// Build tools
	{"make", "build", []string{"--version"}},
	{"cmake", "build", []string{"--version"}},
	{"ninja", "build", []string{"--version"}},
	{"bazel", "build", []string{"version"}},
	{"meson", "build", []string{"--version"}},
	// Compilers
	{"gcc", "compiler", []string{"--version"}},
	{"g++", "compiler", []string{"--version"}},
	{"clang", "compiler", []string{"--version"}},
	{"clang++", "compiler", []string{"--version"}},
	// Containers & cloud
	{"docker", "container", []string{"--version"}},
	{"podman", "container", []string{"--version"}},
	{"kubectl", "cloud", []string{"version", "--client"}},
	{"helm", "cloud", []string{"version", "--short"}},
	{"terraform", "cloud", []string{"--version"}},
	{"aws", "cloud", []string{"--version"}},
	{"gcloud", "cloud", []string{"version"}},
	{"az", "cloud", []string{"--version"}},
	// Linters & formatters
	{"eslint", "lint", []string{"--version"}},
	{"prettier", "lint", []string{"--version"}},
	{"ruff", "lint", []string{"--version"}},
	{"golangci-lint", "lint", []string{"--version"}},
	{"shellcheck", "lint", []string{"--version"}},
	// Databases
	{"psql", "db", []string{"--version"}},
	{"mysql", "db", []string{"--version"}},
	{"sqlite3", "db", []string{"--version"}},
	{"redis-cli", "db", []string{"--version"}},
	// Utilities
	{"curl", "util", []string{"--version"}},
	{"wget", "util", []string{"--version"}},
	{"jq", "util", []string{"--version"}},
	{"htop", "util", []string{"--version"}},
	{"tmux", "util", []string{"-V"}},
	{"ffmpeg", "util", []string{"-version"}},
}

func NewDetectTools() Tool {
	return &detectToolsTool{
		BaseTool: BaseTool{
			SchemaVal: Schema{
				Name:        "detect_tools",
				Description: "Scans the system PATH for common developer toolchains and returns available tools with versions. Use 'filter' to check specific categories: language, pkg, build, compiler, container, cloud, lint, db, util, vcs, or 'all' (default).",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"filter": map[string]interface{}{
							"type":        "string",
							"description": "Optional category filter (comma-separated). E.g. 'language,compiler' or 'all'.",
						},
						"json": map[string]interface{}{
							"type":        "boolean",
							"description": "Return results as JSON instead of text. Default: false.",
						},
					},
				},
			},
			PermLevel:      PermNone,
			HasSideEffects: false,
			MaxOutputChars: 8000,
			ToolCategory:   CategorySystem,
		},
	}
}

type detectToolsTool struct{ BaseTool }

type detectResult struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Path     string `json:"path"`
	Version  string `json:"version,omitempty"`
}

func (t *detectToolsTool) Execute(ctx context.Context, args map[string]interface{}) Result {
	filterStr, _ := args["filter"].(string)
	asJSON, _ := args["json"].(bool)

	var categoryFilter map[string]bool
	if filterStr != "" && filterStr != "all" {
		categoryFilter = make(map[string]bool)
		for _, c := range strings.Split(filterStr, ",") {
			categoryFilter[strings.TrimSpace(c)] = true
		}
	}

	var results []detectResult
	for _, entry := range knownToolList {
		if categoryFilter != nil && !categoryFilter[entry.category] {
			continue
		}
		path, err := exec.LookPath(entry.name)
		if err != nil {
			continue
		}
		binary := path
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			binary = resolved
		}
		version := getToolVersion(entry.name, entry.args, ctx)
		results = append(results, detectResult{
			Name: entry.name, Category: entry.category,
			Path: binary, Version: version,
		})
	}

	if len(results) == 0 {
		msg := "No known developer tools found in PATH."
		if categoryFilter != nil {
			msg = fmt.Sprintf("No tools found for category filter: %s", filterStr)
		}
		return Result{Success: true, Output: msg}
	}

	if asJSON {
		out, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			return Result{Error: fmt.Sprintf("marshal devtools results: %v", err)}
		}
		return Result{Success: true, Output: string(out)}
	}

	// Group by category
	cats := make(map[string][]detectResult)
	var catOrder []string
	for _, r := range results {
		if _, ok := cats[r.Category]; !ok {
			catOrder = append(catOrder, r.Category)
		}
		cats[r.Category] = append(cats[r.Category], r)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d tools:\n", len(results)))
	for _, cat := range catOrder {
		b.WriteString(fmt.Sprintf("\n[%s]\n", cat))
		for _, r := range cats[cat] {
			ver := ""
			if r.Version != "" {
				ver = fmt.Sprintf(" (%s)", r.Version)
			}
			b.WriteString(fmt.Sprintf("  %-16s %s%s\n", r.Name+":", r.Path, ver))
		}
	}
	return Result{Success: true, Output: b.String()}
}

func getToolVersion(name string, args []string, ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(firstLine(buf.String()))
}

// ── read_diff ─────────────────────────────────────────────────────────

func NewReadDiff(workspaceRoot string) Tool {
	return &readDiffTool{
		BaseTool: BaseTool{
			SchemaVal: Schema{
				Name:        "read_diff",
				Description: "Shows git diff output: unstaged changes, staged changes, or diff against a specific branch. Returns structured diff output.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"base": map[string]interface{}{
							"type":        "string",
							"description": "Base ref to diff against (e.g. 'main', 'HEAD~3'). Default: unstaged changes.",
						},
						"staged": map[string]interface{}{
							"type":        "boolean",
							"description": "Show staged (cached) changes instead. Default: false.",
						},
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Optional specific file or directory path to diff.",
						},
					},
				},
			},
			PermLevel:      PermReadOnly,
			HasSideEffects: false,
			MaxOutputChars: 50000,
			ToolCategory:   CategorySystem,
		},
		workspaceRoot: workspaceRoot,
	}
}

type readDiffTool struct {
	BaseTool
	workspaceRoot string
}

func (t *readDiffTool) Execute(ctx context.Context, args map[string]interface{}) Result {
	gitArgs := []string{"diff", "--no-color"}
	staged, _ := args["staged"].(bool)
	if staged {
		gitArgs = append(gitArgs, "--cached")
	}
	if base, ok := args["base"].(string); ok && base != "" {
		if staged {
			gitArgs = []string{"--no-pager", "diff", "--no-color", "--cached", base}
		} else {
			gitArgs = []string{"--no-pager", "diff", "--no-color", base}
		}
	}
	if p, ok := args["path"].(string); ok && p != "" {
		gitArgs = append(gitArgs, "--", p)
	}

	cmd := sandboxCmd(ctx, t.workspaceRoot, "git", gitArgs...)
	cmd.Dir = t.workspaceRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		if len(out) == 0 {
			return Result{Success: true, Output: "No changes (clean working tree)."}
		}
		return Result{Success: false, Error: fmt.Sprintf("git diff: %v", err), Output: string(out)}
	}
	outStr := string(out)
	if strings.TrimSpace(outStr) == "" {
		return Result{Success: true, Output: "No changes."}
	}
	return Result{Success: true, Output: outStr}
}

// ── run_linter ────────────────────────────────────────────────────────

func NewRunLinter(workspaceRoot string) Tool {
	return &runLinterTool{
		BaseTool: BaseTool{
			SchemaVal: Schema{
				Name:        "run_linter",
				Description: "Runs a linter on the workspace and returns structured findings. Supports eslint, ruff, clippy, and golangci-lint.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"linter": map[string]interface{}{
							"type":        "string",
							"description": "Linter to run: 'eslint', 'ruff', 'clippy', 'golangci-lint', or 'auto' (default: auto-detect).",
						},
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Optional subdirectory or file to lint.",
						},
					},
				},
			},
			PermLevel:      PermExecute,
			HasSideEffects: true,
			MaxOutputChars: 20000,
			ToolCategory:   CategorySystem,
		},
		workspaceRoot: workspaceRoot,
	}
}

type runLinterTool struct {
	BaseTool
	workspaceRoot string
}

func (t *runLinterTool) Execute(ctx context.Context, args map[string]interface{}) Result {
	linter, _ := args["linter"].(string)
	target, _ := args["path"].(string)

	if linter == "" || linter == "auto" {
		linter = detectLinter(t.workspaceRoot)
	}
	if linter == "" {
		return Result{Success: false, Error: "No linter detected in workspace. Available: eslint, ruff, clippy, golangci-lint."}
	}

	var cmdArgs []string
	switch linter {
	case "eslint":
		cmdArgs = []string{"npx", "eslint", "--format", "compact"}
	case "ruff":
		cmdArgs = []string{"ruff", "check", "--output-format", "concise"}
	case "clippy":
		cmdArgs = []string{"cargo", "clippy", "--message-format", "short"}
	case "golangci-lint":
		cmdArgs = []string{"golangci-lint", "run", "--out-format", "colored-line-number"}
	default:
		return Result{Success: false, Error: fmt.Sprintf("unsupported linter: %s", linter)}
	}
	if target != "" {
		cmdArgs = append(cmdArgs, target)
	}

	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	cmd := sandboxCmd(ctx, t.workspaceRoot, cmdArgs[0], cmdArgs[1:]...)
	cmd.Dir = t.workspaceRoot
	out, err := cmd.CombinedOutput()
	outStr := strings.TrimSpace(string(out))
	if err != nil && outStr == "" {
		return Result{Success: false, Error: fmt.Sprintf("%s: %v", linter, err)}
	}
	if outStr == "" {
		return Result{Success: true, Output: fmt.Sprintf("%s: No issues found.", linter)}
	}
	return Result{Success: true, Output: fmt.Sprintf("%s findings:\n%s", linter, outStr)}
}

func detectLinter(root string) string {
	if _, err := os.Stat(filepath.Join(root, "package.json")); err == nil {
		if _, err := os.Stat(filepath.Join(root, "node_modules", ".bin", "eslint")); err == nil {
			return "eslint"
		}
	}
	if _, err := os.Stat(filepath.Join(root, "Cargo.toml")); err == nil {
		return "clippy"
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		return "golangci-lint"
	}
	if _, err := os.Stat(filepath.Join(root, "pyproject.toml")); err == nil {
		return "ruff"
	}
	return ""
}

// ── workspace_state ───────────────────────────────────────────────────

func NewWorkspaceState(workspaceRoot string) Tool {
	return &workspaceStateTool{
		BaseTool: BaseTool{
			SchemaVal: Schema{
				Name:        "workspace_state",
				Description: "Returns an overview of the workspace: current git branch, status summary, recent commits, and top-level file tree.",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
			PermLevel:      PermReadOnly,
			HasSideEffects: false,
			MaxOutputChars: 10000,
			ToolCategory:   CategorySystem,
		},
		workspaceRoot: workspaceRoot,
	}
}

type workspaceStateTool struct {
	BaseTool
	workspaceRoot string
}

func (t *workspaceStateTool) Execute(ctx context.Context, args map[string]interface{}) Result {
	var b strings.Builder

	// Git branch
	b.WriteString("=== Git Status ===\n")
	cmd := sandboxCmd(ctx, t.workspaceRoot, "git", "branch", "--show-current")
	cmd.Dir = t.workspaceRoot
	if out, err := cmd.Output(); err == nil {
		b.WriteString(fmt.Sprintf("Branch: %s\n", strings.TrimSpace(string(out))))
	}
	cmd = sandboxCmd(ctx, t.workspaceRoot, "git", "status", "--short")
	cmd.Dir = t.workspaceRoot
	if out, err := cmd.Output(); err == nil {
		status := string(out)
		if strings.TrimSpace(status) == "" {
			b.WriteString("Status: clean\n")
		} else {
			b.WriteString("Status:\n" + status)
		}
	}

	// Recent commits
	b.WriteString("\n=== Recent Commits ===\n")
	cmd = sandboxCmd(ctx, t.workspaceRoot, "git", "log", "--oneline", "-10")
	cmd.Dir = t.workspaceRoot
	if out, err := cmd.Output(); err == nil {
		b.WriteString(string(out))
	}

	// Top-level file tree (1 level deep)
	b.WriteString("\n=== Workspace Files ===\n")
	entries, err := os.ReadDir(t.workspaceRoot)
	if err != nil {
		b.WriteString(fmt.Sprintf("(error reading: %v)\n", err))
	} else {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if e.IsDir() {
				b.WriteString(fmt.Sprintf("  %s/\n", e.Name()))
			} else {
				b.WriteString(fmt.Sprintf("  %s\n", e.Name()))
			}
		}
	}

	return Result{Success: true, Output: b.String()}
}

// ── update_memory_md ──────────────────────────────────────────────────

// Memory files that are safe for the agent to edit.
var allowedMemoryFiles = map[string]bool{
	"MEMORY.md":    true,
	"SKILL.md":     true,
	"CLAUDE.md":    true,
	"CODEBUDDY.md": true,
	"AGENTS.md":    true,
}

func NewUpdateMemoryMD(workspaceRoot string) Tool {
	return &updateMemoryMDTool{
		BaseTool: BaseTool{
			SchemaVal: Schema{
				Name:        "update_memory_md",
				Description: "Updates persistent memory files in the workspace. Supports upsert (replace section), append (add after section), and delete (remove section) operations on MEMORY.md, CLAUDE.md, AGENTS.md, SKILL.md, or CODEBUDDY.md.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"file": map[string]interface{}{
							"type":        "string",
							"description": "Target file: MEMORY.md, CLAUDE.md, AGENTS.md, SKILL.md, or CODEBUDDY.md (default: MEMORY.md).",
						},
						"section": map[string]interface{}{
							"type":        "string",
							"description": "Section heading to operate on (e.g. '## Preferences', '### Rules').",
						},
						"content": map[string]interface{}{
							"type":        "string",
							"description": "Content to write. Required for upsert and append operations.",
						},
						"operation": map[string]interface{}{
							"type":        "string",
							"description": "Operation: 'upsert' (replace section, creates if missing — default), 'append' (add content after existing section), 'delete' (remove section entirely).",
						},
					},
					"required": []string{"section"},
				},
			},
			PermLevel:      PermWrite,
			HasSideEffects: true,
			MaxOutputChars: 3000,
			ToolCategory:   CategorySystem,
		},
		workspaceRoot: workspaceRoot,
	}
}

type updateMemoryMDTool struct {
	BaseTool
	workspaceRoot string
}

func (t *updateMemoryMDTool) Execute(ctx context.Context, args map[string]interface{}) Result {
	fileName, _ := args["file"].(string)
	if fileName == "" {
		fileName = "MEMORY.md"
	}
	section, _ := args["section"].(string)
	content, _ := args["content"].(string)
	operation, _ := args["operation"].(string)
	if operation == "" {
		operation = "upsert"
	}

	// Security: only allow specific files
	if !allowedMemoryFiles[fileName] {
		return Result{Success: false, Error: fmt.Sprintf("file %q is not allowed — must be one of: MEMORY.md, CLAUDE.md, AGENTS.md, SKILL.md, CODEBUDDY.md", fileName)}
	}

	// Normalize section heading
	if !strings.HasPrefix(section, "#") {
		section = "## " + section
	}

	if (operation == "upsert" || operation == "append") && content == "" {
		return Result{Success: false, Error: "content is required for upsert and append operations"}
	}

	if len(content) > 100000 {
		return Result{Success: false, Error: fmt.Sprintf("content too large (%d chars, max 100000)", len(content))}
	}

	filePath := filepath.Join(t.workspaceRoot, fileName)
	resolvedPath, err := security.ValidatePath(filePath, t.workspaceRoot)
	if err != nil {
		return Result{Error: fmt.Sprintf("path blocked by security policy: %v", err)}
	}
	existing, err := os.ReadFile(resolvedPath)
	if err != nil && !os.IsNotExist(err) {
		return Result{Success: false, Error: fmt.Sprintf("read %s: %v", fileName, err)}
	}

	current := string(existing)
	var newContent string

	switch operation {
	case "upsert":
		newContent = upsertSection(current, section, content)
	case "append":
		newContent = appendToSection(current, section, content)
	case "delete":
		newContent = deleteSection(current, section)
	default:
		return Result{Success: false, Error: fmt.Sprintf("unknown operation: %s (use upsert, append, or delete)", operation)}
	}

	if err := os.MkdirAll(filepath.Dir(resolvedPath), 0755); err != nil {
		return Result{Success: false, Error: fmt.Sprintf("mkdir: %v", err)}
	}
	if err := os.WriteFile(resolvedPath, []byte(newContent), 0644); err != nil {
		return Result{Success: false, Error: fmt.Sprintf("write %s: %v", fileName, err)}
	}

	if existing == nil {
		return Result{Success: true, Output: fmt.Sprintf("Created %s with section %s", fileName, section)}
	}

	switch operation {
	case "delete":
		return Result{Success: true, Output: fmt.Sprintf("Deleted section %s from %s", section, fileName)}
	case "append":
		return Result{Success: true, Output: fmt.Sprintf("Appended to section %s in %s", section, fileName)}
	default:
		return Result{Success: true, Output: fmt.Sprintf("Updated section %s in %s", section, fileName)}
	}
}

// upsertSection replaces or appends a section in a markdown document.
func upsertSection(doc, section, content string) string {
	start, end := findSectionBounds(doc, section)
	if start < 0 {
		if doc != "" && !strings.HasSuffix(doc, "\n") {
			doc += "\n"
		}
		return doc + "\n" + section + "\n" + content + "\n"
	}
	prefix := doc[:start]
	suffix := ""
	if end < len(doc) {
		suffix = doc[end:]
	}
	return prefix + section + "\n" + content + "\n" + suffix
}

// appendToSection adds content after an existing section. If section doesn't
// exist, it creates it.
func appendToSection(doc, section, content string) string {
	start, end := findSectionBounds(doc, section)
	if start < 0 {
		return upsertSection(doc, section, content)
	}
	// Insert content right before the next section (at `end`).
	prefix := doc[:end]
	suffix := ""
	if end < len(doc) {
		suffix = doc[end:]
	}
	return prefix + content + "\n" + suffix
}

// deleteSection removes a section and its content entirely.
func deleteSection(doc, section string) string {
	start, end := findSectionBounds(doc, section)
	if start < 0 {
		return doc // section not found, nothing to delete
	}
	return doc[:start] + doc[end:]
}

// findSectionBounds finds the start (position of section heading) and end
// (start of next heading at the same or higher level, or end of doc).
func findSectionBounds(doc, section string) (start, end int) {
	search := section + "\n"
	start = strings.Index(doc, search)
	if start < 0 {
		search = section + "\r\n"
		start = strings.Index(doc, search)
	}
	if start < 0 {
		return -1, -1
	}

	level := headingLevel(section)
	afterSection := doc[start+len(section):]
	end = len(doc)
	remaining := afterSection
	offset := 0
	for {
		idx := strings.Index(remaining, "\n#")
		if idx < 0 {
			break
		}
		lineStart := offset + idx + 1
		lineEnd := strings.Index(afterSection[lineStart:], "\n")
		if lineEnd < 0 {
			lineEnd = len(afterSection) - lineStart
		}
		line := afterSection[lineStart : lineStart+lineEnd]
		if strings.HasPrefix(line, "#") && headingLevel(line) <= level {
			end = start + len(section) + lineStart
			break
		}
		offset = lineStart + lineEnd
		remaining = afterSection[offset:]
	}
	return start, end
}

func headingLevel(h string) int {
	l := 0
	for _, c := range h {
		if c == '#' {
			l++
		} else {
			break
		}
	}
	return l
}

// ── csv_export ────────────────────────────────────────────────────────

func NewCSVExport(workspaceRoot string) Tool {
	return &csvExportTool{
		BaseTool: BaseTool{
			SchemaVal: Schema{
				Name:        "csv_export",
				Description: "Exports a JSON array of objects to a CSV file in the workspace.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"data": map[string]interface{}{
							"type":        "string",
							"description": "JSON string representing an array of objects (e.g. '[{\"name\":\"Alice\",\"score\":42}]').",
						},
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Destination path relative to workspace (e.g. 'output.csv').",
						},
						"delimiter": map[string]interface{}{
							"type":        "string",
							"description": "CSV delimiter (default: ',').",
						},
					},
					"required": []string{"data", "path"},
				},
			},
			PermLevel:      PermWrite,
			HasSideEffects: true,
			MaxOutputChars: 2000,
			ToolCategory:   CategorySystem,
		},
		workspaceRoot: workspaceRoot,
	}
}

type csvExportTool struct {
	BaseTool
	workspaceRoot string
}

func (t *csvExportTool) Execute(ctx context.Context, args map[string]interface{}) Result {
	dataStr, _ := args["data"].(string)
	relPath, _ := args["path"].(string)
	delimiter, _ := args["delimiter"].(string)
	if delimiter == "" {
		delimiter = ","
	}
	if dataStr == "" || relPath == "" {
		return Result{Success: false, Error: "data and path are required"}
	}

	// Parse JSON array — expecting []map[string]interface{}
	var records []map[string]interface{}
	// The JSON may come as a string or embedded; try direct unmarshal.
	decoder := json.NewDecoder(strings.NewReader(dataStr))
	if err := decoder.Decode(&records); err != nil {
		// Try with json.Unmarshal as fallback
		if err2 := json.Unmarshal([]byte(dataStr), &records); err2 != nil {
			return Result{Success: false, Error: fmt.Sprintf("invalid JSON: %v", err)}
		}
	}
	if len(records) == 0 {
		return Result{Success: false, Error: "JSON array is empty"}
	}

	// Collect all column headers
	headers := collectHeaders(records)
	fullPath := filepath.Join(t.workspaceRoot, relPath)
	resolvedPath, err := security.ValidatePath(fullPath, t.workspaceRoot)
	if err != nil {
		return Result{Error: fmt.Sprintf("path blocked: %v", err)}
	}

	var buf bytes.Buffer
	buf.WriteString(strings.Join(headers, delimiter) + "\n")
	for _, rec := range records {
		row := make([]string, len(headers))
		for i, h := range headers {
			val := ""
			if v, ok := rec[h]; ok {
				val = fmt.Sprint(v)
			}
			row[i] = csvEscape(val, delimiter)
		}
		buf.WriteString(strings.Join(row, delimiter) + "\n")
	}

	if err := os.WriteFile(resolvedPath, buf.Bytes(), 0644); err != nil {
		return Result{Success: false, Error: fmt.Sprintf("write: %v", err)}
	}
	return Result{Success: true, Output: fmt.Sprintf("Exported %d rows × %d columns to %s", len(records), len(headers), relPath)}
}

func collectHeaders(records []map[string]interface{}) []string {
	seen := make(map[string]bool)
	var headers []string
	for _, r := range records {
		for k := range r {
			if !seen[k] {
				seen[k] = true
				headers = append(headers, k)
			}
		}
	}
	return headers
}

func csvEscape(val, delim string) string {
	if strings.ContainsAny(val, delim+"\"\n") {
		val = strings.ReplaceAll(val, "\"", "\"\"")
		return "\"" + val + "\""
	}
	return val
}

// ── helpers ───────────────────────────────────────────────────────────

func firstLine(s string) string {
	if idx := strings.Index(s, "\n"); idx >= 0 {
		return s[:idx]
	}
	return s
}
