// Package eino provides an adapter layer that maps Mneme config to
// cloudwego/eino chat model instances and agent definitions.
//
// NOTE: The eino-ext built-in tool packages (filesystem, shell, duckduckgo)
// do not yet exist in the eino-ext module (currently at v0.0.1-alpha).
// When these packages become available in a future eino-ext release, the
// file/shell tool adapters below (read_file, write_file, edit_file, glob,
// grep, shell) should be replaced with eino-ext native implementations.
// The current adapters using utils.InferTool remain as the canonical
// implementation until then.
package eino

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/simon/mneme/internal/memory"

	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/internal/security"
	"github.com/simon/mneme/internal/tools"
)

// ToolConfig holds the dependencies needed to construct the full set of
// adapted core tools. Every field is optional — sensible zero-value defaults
// are applied for any field left unset.
type ToolConfig struct {
	// Workspace is the root directory for file-system-scoped tools.
	Workspace string

	// Tier controls the security tier used by the shell tool (default: TierFull).
	Tier security.Tier

	// ProxyConfig is used by HTTPGet and HTTPPost tools.
	ProxyConfig config.ProxyConfig

	// ShellConfig controls shell tool behaviour (max output, safe env vars).
	ShellConfig config.ToolsShellConfig

	// SandboxConfig selects the sandbox backend for command execution.
	SandboxConfig config.SandboxConfig

	// BraveAPIKey enables the Brave search backend in WebSearch.
	BraveAPIKey string

	// TavilyAPIKey enables the Tavily search backend in WebSearch.
	TavilyAPIKey string

	// SearxngURL enables the SearxNG search backend in WebSearch.
	SearxngURL string

	// MemoryPipeline is the runtime memory pipeline. When nil, memory tools
	// return placeholder messages.
	MemoryPipeline *memory.Pipeline

	// RuntimeConfig enables config introspection tools (config_snapshot,
	// config_autonomy, config_data_paths). When nil, config tools are omitted.
	RuntimeConfig *config.Config
}

// toResult converts a tools.Result to a (string, error) pair suitable for
// eino's InvokableTool contract. A non-success result is mapped to an error
// so the model can see the failure reason.
func toResult(r tools.Result) (string, error) {
	if r.Success {
		return r.Output, nil
	}
	if r.Output != "" {
		return r.Output, fmt.Errorf("%s", r.Error)
	}
	return "", fmt.Errorf("%s", r.Error)
}

// CollectTools instantiates the full set of core tools, adapts each one to
// eino's tool.BaseTool via utils.InferTool, and returns the slice ready to
// be mounted on a ChatModel with WithTools.
//
// Tools are constructed with realistic defaults: unset API keys fall back
// to DuckDuckGo for web search, an empty sandbox config auto-detects the
// best available backend, and the tier defaults to TierFull.
func CollectTools(cfg *ToolConfig) []tool.BaseTool {
	if cfg == nil {
		cfg = &ToolConfig{}
	}

	tier := cfg.Tier
	if tier == "" {
		tier = security.TierFull
	}

	w := cfg.Workspace // may be empty — tools handle empty workspace gracefully

	// ── Instantiate all core tools ───────────────────────────────────────

	// Search. The `browser` tool is provided by the browser-cdp extension
	// (real rendering) rather than a core in-process HTTP fetcher, to avoid a
	// same-name tool collision with different capabilities.
	webSearch := tools.NewWebSearch(cfg.BraveAPIKey, cfg.TavilyAPIKey, cfg.SearxngURL)

	// System
	currentTime := tools.NewCurrentTime()
	askUser := tools.NewAskUser()
	waitTool := tools.NewWait()
	detectTools := tools.NewDetectTools()

	// Network
	httpGet := tools.NewHTTPGet(cfg.ProxyConfig)
	httpPost := tools.NewHTTPPost(cfg.ProxyConfig)

	// File
	readFile := tools.NewReadFile(w)
	writeFile := tools.NewWriteFile(w)
	editFile := tools.NewEditFile(w)
	listDir := tools.NewListDir(w)

	// Dev
	glob := tools.NewGlob(w)
	grep := tools.NewGrep(w)
	shell := tools.NewShell(w, tier, cfg.ShellConfig, cfg.SandboxConfig)
	gitOps := tools.NewGitOps(w)
	readDiff := tools.NewReadDiff(w)
	runLinter := tools.NewRunLinter(w)
	runTests := tools.NewRunTests(w)
	workspaceState := tools.NewWorkspaceState(w)
	applyPatch := tools.NewApplyPatch(w)
	updateMemoryMD := tools.NewUpdateMemoryMD(w)
	csvExport := tools.NewCSVExport(w)

	// ── Adapt each tool via utils.InferTool ──────────────────────────────

	adapters := []func() tool.BaseTool{
		// ── search ───────────────────────────────────────────────────
		func() tool.BaseTool { return adaptWebSearch(webSearch) },

		// ── system ──────────────────────────────────────────────────
		func() tool.BaseTool { return adaptCurrentTime(currentTime) },
		func() tool.BaseTool { return adaptAskUser(askUser) },
		func() tool.BaseTool { return adaptWait(waitTool) },
		func() tool.BaseTool { return adaptDetectTools(detectTools) },

		// ── network ─────────────────────────────────────────────────
		func() tool.BaseTool { return adaptHTTPGet(httpGet) },
		func() tool.BaseTool { return adaptHTTPPost(httpPost) },

		// ── file ────────────────────────────────────────────────────
		func() tool.BaseTool { return adaptReadFile(readFile) },
		func() tool.BaseTool { return adaptWriteFile(writeFile) },
		func() tool.BaseTool { return adaptEditFile(editFile) },
		func() tool.BaseTool { return adaptListDir(listDir) },

		// ── dev ─────────────────────────────────────────────────────
		func() tool.BaseTool { return adaptGlob(glob) },
		func() tool.BaseTool { return adaptGrep(grep) },
		func() tool.BaseTool { return adaptShell(shell) },
		func() tool.BaseTool { return adaptGit(gitOps) },
		func() tool.BaseTool { return adaptReadDiff(readDiff) },
		func() tool.BaseTool { return adaptRunLinter(runLinter) },
		func() tool.BaseTool { return adaptRunTests(runTests) },
		func() tool.BaseTool { return adaptWorkspaceState(workspaceState) },
		func() tool.BaseTool { return adaptApplyPatch(applyPatch) },
		func() tool.BaseTool { return adaptUpdateMemoryMD(updateMemoryMD) },
		func() tool.BaseTool { return adaptCSVExport(csvExport) },
	}

	out := make([]tool.BaseTool, 0, len(adapters))
	for _, fn := range adapters {
		t := fn()
		if t != nil {
			out = append(out, t)
		}
	}

	out = append(out, adaptBusinessTools(cfg)...)
	out = append(out, adaptMemoryTools(cfg)...)
	out = append(out, adaptConfigTools(cfg)...)

	return out
}

// ── search input types ──────────────────────────────────────────────────

type webSearchInput struct {
	Query string `json:"query" jsonschema:"description=Search query"`
	Count int    `json:"count,omitempty" jsonschema:"description=Number of results (default 5, max 20)"`
}

func adaptWebSearch(t *tools.WebSearch) tool.BaseTool {
	tool, err := utils.InferTool("web_search",
		"Search the web for information. Returns titles, URLs, and snippets.",
		func(ctx context.Context, in webSearchInput) (string, error) {
			return toResult(t.Execute(ctx, map[string]interface{}{
				"query": in.Query,
				"count": float64(in.Count),
			}))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt web_search", "err", err)
		return nil
	}
	return tool
}

// ── system input types ──────────────────────────────────────────────────

func adaptCurrentTime(t tools.Tool) tool.BaseTool {
	tool, err := utils.InferTool("current_time",
		"Returns the current date and time in ISO 8601 format. Use this when you need to know the current time for scheduling, time-based logic, or date calculations.",
		func(ctx context.Context, _ struct{}) (string, error) {
			return toResult(t.Execute(ctx, nil))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt current_time", "err", err)
		return nil
	}
	return tool
}

type askUserInput struct {
	Question string   `json:"question" jsonschema:"description=The question to ask the user"`
	Options  []string `json:"options,omitempty" jsonschema:"description=Optional list of choices for the user (max 5)"`
}

func adaptAskUser(t tools.Tool) tool.BaseTool {
	tool, err := utils.InferTool("ask_user",
		"Ask the user a clarifying question. The turn pauses until the user responds.",
		func(ctx context.Context, in askUserInput) (string, error) {
			opts := make([]interface{}, len(in.Options))
			for i, o := range in.Options {
				opts[i] = o
			}
			return toResult(t.Execute(ctx, map[string]interface{}{
				"question": in.Question,
				"options":  opts,
			}))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt ask_user", "err", err)
		return nil
	}
	return tool
}

type waitInput struct {
	Seconds float64 `json:"seconds" jsonschema:"description=Seconds to wait (max 30)"`
}

func adaptWait(t tools.Tool) tool.BaseTool {
	tool, err := utils.InferTool("wait",
		"Pause execution for a specified number of seconds. Use sparingly between dependent operations.",
		func(ctx context.Context, in waitInput) (string, error) {
			return toResult(t.Execute(ctx, map[string]interface{}{
				"seconds": in.Seconds,
			}))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt wait", "err", err)
		return nil
	}
	return tool
}

type detectToolsInput struct {
	Filter string `json:"filter,omitempty" jsonschema:"description=Optional category filter (comma-separated). E.g. 'language,compiler' or 'all'"`
	JSON   bool   `json:"json,omitempty" jsonschema:"description=Return results as JSON instead of text. Default: false"`
}

func adaptDetectTools(t tools.Tool) tool.BaseTool {
	tool, err := utils.InferTool("detect_tools",
		"Scans the system PATH for common developer toolchains and returns available tools with versions.",
		func(ctx context.Context, in detectToolsInput) (string, error) {
			return toResult(t.Execute(ctx, map[string]interface{}{
				"filter": in.Filter,
				"json":   in.JSON,
			}))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt detect_tools", "err", err)
		return nil
	}
	return tool
}

// ── network input types ─────────────────────────────────────────────────

type httpGetInput struct {
	URL string `json:"url" jsonschema:"description=URL to fetch"`
}

func adaptHTTPGet(t *tools.HTTPGet) tool.BaseTool {
	tool, err := utils.InferTool("http_get",
		"Perform an HTTP GET request to the given URL. Returns the status code and response body (up to 1MB).",
		func(ctx context.Context, in httpGetInput) (string, error) {
			return toResult(t.Execute(ctx, map[string]interface{}{
				"url": in.URL,
			}))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt http_get", "err", err)
		return nil
	}
	return tool
}

type httpPostInput struct {
	URL  string `json:"url" jsonschema:"description=URL to post to"`
	Body string `json:"body" jsonschema:"description=JSON body string"`
}

func adaptHTTPPost(t *tools.HTTPPost) tool.BaseTool {
	tool, err := utils.InferTool("http_post",
		"Perform an HTTP POST request with a JSON body. Returns the status code and response body (up to 1MB).",
		func(ctx context.Context, in httpPostInput) (string, error) {
			return toResult(t.Execute(ctx, map[string]interface{}{
				"url":  in.URL,
				"body": in.Body,
			}))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt http_post", "err", err)
		return nil
	}
	return tool
}

// ── file input types ────────────────────────────────────────────────────

type readFileInput struct {
	Path string `json:"path" jsonschema:"description=Path to the file to read"`
}

func adaptReadFile(t *tools.ReadFile) tool.BaseTool {
	tool, err := utils.InferTool("read_file",
		"Read the contents of a file (up to 5MB).",
		func(ctx context.Context, in readFileInput) (string, error) {
			return toResult(t.Execute(ctx, map[string]interface{}{
				"path": in.Path,
			}))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt read_file", "err", err)
		return nil
	}
	return tool
}

type writeFileInput struct {
	Path    string `json:"path" jsonschema:"description=Path to the file to write"`
	Content string `json:"content" jsonschema:"description=Content to write to the file"`
}

func adaptWriteFile(t *tools.WriteFile) tool.BaseTool {
	tool, err := utils.InferTool("write_file",
		"Write content to a file, creating it if needed (creates parent directories).",
		func(ctx context.Context, in writeFileInput) (string, error) {
			return toResult(t.Execute(ctx, map[string]interface{}{
				"path":    in.Path,
				"content": in.Content,
			}))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt write_file", "err", err)
		return nil
	}
	return tool
}

type editFileInput struct {
	Path       string `json:"path" jsonschema:"description=File path relative to workspace root"`
	OldString  string `json:"old_string" jsonschema:"description=Exact text to find and replace"`
	NewString  string `json:"new_string" jsonschema:"description=Replacement text"`
	ReplaceAll bool   `json:"replace_all,omitempty" jsonschema:"description=Replace all occurrences (default: false, requires single match)"`
}

func adaptEditFile(t *tools.EditFile) tool.BaseTool {
	tool, err := utils.InferTool("edit_file",
		"Performs exact string replacement in a file. Finds old_string and replaces it with new_string.",
		func(ctx context.Context, in editFileInput) (string, error) {
			return toResult(t.Execute(ctx, map[string]interface{}{
				"path":        in.Path,
				"old_string":  in.OldString,
				"new_string":  in.NewString,
				"replace_all": in.ReplaceAll,
			}))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt edit_file", "err", err)
		return nil
	}
	return tool
}

type listDirInput struct {
	Path string `json:"path,omitempty" jsonschema:"description=Path to list (defaults to workspace root)"`
}

func adaptListDir(t *tools.ListDir) tool.BaseTool {
	tool, err := utils.InferTool("list_dir",
		"List files and directories in a given path.",
		func(ctx context.Context, in listDirInput) (string, error) {
			return toResult(t.Execute(ctx, map[string]interface{}{
				"path": in.Path,
			}))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt list_dir", "err", err)
		return nil
	}
	return tool
}

// ── dev input types ─────────────────────────────────────────────────────

type globInput struct {
	Pattern string `json:"pattern" jsonschema:"description=Glob pattern (e.g. '**/*.go', 'src/**/*.tsx', '*.md')"`
	Path    string `json:"path,omitempty" jsonschema:"description=Optional subdirectory to search within (default: whole workspace)"`
}

func adaptGlob(t *tools.Glob) tool.BaseTool {
	tool, err := utils.InferTool("glob",
		"Finds files matching a glob pattern within the workspace. Returns sorted relative file paths.",
		func(ctx context.Context, in globInput) (string, error) {
			return toResult(t.Execute(ctx, map[string]interface{}{
				"pattern": in.Pattern,
				"path":    in.Path,
			}))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt glob", "err", err)
		return nil
	}
	return tool
}

type grepInput struct {
	Pattern       string `json:"pattern" jsonschema:"description=Text or regex pattern to search for"`
	Path          string `json:"path,omitempty" jsonschema:"description=Optional subdirectory to search within (default: whole workspace)"`
	Include       string `json:"include,omitempty" jsonschema:"description=Optional file glob pattern to include (e.g. '*.go', '*.{ts,tsx}')"`
	CaseSensitive bool   `json:"case_sensitive,omitempty" jsonschema:"description=Whether the search is case-sensitive (default: false)"`
	Regex         bool   `json:"regex,omitempty" jsonschema:"description=Treat pattern as regex (default: false)"`
}

func adaptGrep(t *tools.Grep) tool.BaseTool {
	tool, err := utils.InferTool("grep",
		"Searches for a pattern (text or regex) in files within the workspace. Returns matching lines with file paths and line numbers.",
		func(ctx context.Context, in grepInput) (string, error) {
			return toResult(t.Execute(ctx, map[string]interface{}{
				"pattern":        in.Pattern,
				"path":           in.Path,
				"include":        in.Include,
				"case_sensitive": in.CaseSensitive,
				"regex":          in.Regex,
			}))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt grep", "err", err)
		return nil
	}
	return tool
}

type shellInput struct {
	Command string `json:"command" jsonschema:"description=Shell command to execute"`
}

func adaptShell(t *tools.Shell) tool.BaseTool {
	tool, err := utils.InferTool("shell",
		"Execute a shell command within the workspace (subject to security tier restrictions).",
		func(ctx context.Context, in shellInput) (string, error) {
			return toResult(t.Execute(ctx, map[string]interface{}{
				"command": in.Command,
			}))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt shell", "err", err)
		return nil
	}
	return tool
}

type gitInput struct {
	Command string `json:"command" jsonschema:"description=Git subcommand: status, diff, log, branch, add, commit, show, stash"`
	Args    string `json:"args,omitempty" jsonschema:"description=Additional arguments for the git command"`
}

func adaptGit(t *tools.GitOps) tool.BaseTool {
	tool, err := utils.InferTool("git",
		"Run git commands: status, diff, log, branch, add, commit, show, stash.",
		func(ctx context.Context, in gitInput) (string, error) {
			return toResult(t.Execute(ctx, map[string]interface{}{
				"command": in.Command,
				"args":    in.Args,
			}))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt git", "err", err)
		return nil
	}
	return tool
}

type readDiffInput struct {
	Base   string `json:"base,omitempty" jsonschema:"description=Base ref to diff against (e.g. 'main', 'HEAD~3'). Default: unstaged changes"`
	Staged bool   `json:"staged,omitempty" jsonschema:"description=Show staged (cached) changes instead. Default: false"`
	Path   string `json:"path,omitempty" jsonschema:"description=Optional specific file or directory path to diff"`
}

func adaptReadDiff(t tools.Tool) tool.BaseTool {
	tool, err := utils.InferTool("read_diff",
		"Shows git diff output: unstaged changes, staged changes, or diff against a specific branch.",
		func(ctx context.Context, in readDiffInput) (string, error) {
			return toResult(t.Execute(ctx, map[string]interface{}{
				"base":   in.Base,
				"staged": in.Staged,
				"path":   in.Path,
			}))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt read_diff", "err", err)
		return nil
	}
	return tool
}

type runLinterInput struct {
	Linter string `json:"linter,omitempty" jsonschema:"description=Linter to run: 'eslint', 'ruff', 'clippy', 'golangci-lint', or 'auto' (default: auto-detect)"`
	Path   string `json:"path,omitempty" jsonschema:"description=Optional subdirectory or file to lint"`
}

func adaptRunLinter(t tools.Tool) tool.BaseTool {
	tool, err := utils.InferTool("run_linter",
		"Runs a linter on the workspace and returns structured findings. Supports eslint, ruff, clippy, and golangci-lint.",
		func(ctx context.Context, in runLinterInput) (string, error) {
			return toResult(t.Execute(ctx, map[string]interface{}{
				"linter": in.Linter,
				"path":   in.Path,
			}))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt run_linter", "err", err)
		return nil
	}
	return tool
}

type runTestsInput struct {
	Framework string `json:"framework,omitempty" jsonschema:"description=Test framework: 'go', 'cargo', 'vitest', 'jest', 'pytest', or 'auto' (default: auto-detect)"`
	Filter    string `json:"filter,omitempty" jsonschema:"description=Optional test name/pattern filter"`
	Path      string `json:"path,omitempty" jsonschema:"description=Optional subdirectory or specific test file"`
}

func adaptRunTests(t tools.Tool) tool.BaseTool {
	tool, err := utils.InferTool("run_tests",
		"Runs the test suite for the workspace. Supports Go, Rust, JS/TS, and Python.",
		func(ctx context.Context, in runTestsInput) (string, error) {
			return toResult(t.Execute(ctx, map[string]interface{}{
				"framework": in.Framework,
				"filter":    in.Filter,
				"path":      in.Path,
			}))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt run_tests", "err", err)
		return nil
	}
	return tool
}

func adaptWorkspaceState(t tools.Tool) tool.BaseTool {
	tool, err := utils.InferTool("workspace_state",
		"Returns an overview of the workspace: current git branch, status summary, recent commits, and top-level file tree.",
		func(ctx context.Context, _ struct{}) (string, error) {
			return toResult(t.Execute(ctx, nil))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt workspace_state", "err", err)
		return nil
	}
	return tool
}

type applyPatchInput struct {
	Patch  string `json:"patch" jsonschema:"description=Unified diff patch content"`
	Target string `json:"target,omitempty" jsonschema:"description=Target file to patch (relative to workspace)"`
}

func adaptApplyPatch(t *tools.ApplyPatch) tool.BaseTool {
	tool, err := utils.InferTool("apply_patch",
		"Apply a unified diff patch to files in the workspace.",
		func(ctx context.Context, in applyPatchInput) (string, error) {
			return toResult(t.Execute(ctx, map[string]interface{}{
				"patch":  in.Patch,
				"target": in.Target,
			}))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt apply_patch", "err", err)
		return nil
	}
	return tool
}

type updateMemoryMDInput struct {
	File      string `json:"file,omitempty" jsonschema:"description=Target file: MEMORY.md, CLAUDE.md, AGENTS.md, SKILL.md, or CODEBUDDY.md (default: MEMORY.md)"`
	Section   string `json:"section" jsonschema:"description=Section heading to operate on (e.g. '## Preferences', '### Rules')"`
	Content   string `json:"content,omitempty" jsonschema:"description=Content to write. Required for upsert and append operations"`
	Operation string `json:"operation,omitempty" jsonschema:"description=Operation: 'upsert' (replace section, creates if missing — default), 'append' (add content after existing section), 'delete' (remove section entirely)"`
}

func adaptUpdateMemoryMD(t tools.Tool) tool.BaseTool {
	tool, err := utils.InferTool("update_memory_md",
		"Updates persistent memory files in the workspace. Supports upsert, append, and delete operations on MEMORY.md, CLAUDE.md, AGENTS.md, SKILL.md, or CODEBUDDY.md.",
		func(ctx context.Context, in updateMemoryMDInput) (string, error) {
			return toResult(t.Execute(ctx, map[string]interface{}{
				"file":      in.File,
				"section":   in.Section,
				"content":   in.Content,
				"operation": in.Operation,
			}))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt update_memory_md", "err", err)
		return nil
	}
	return tool
}

type csvExportInput struct {
	Data      string `json:"data" jsonschema:"description=JSON string representing an array of objects (e.g. '[{\"name\":\"Alice\",\"score\":42}]')"`
	Path      string `json:"path" jsonschema:"description=Destination path relative to workspace (e.g. 'output.csv')"`
	Delimiter string `json:"delimiter,omitempty" jsonschema:"description=CSV delimiter (default: ',')"`
}

func adaptCSVExport(t tools.Tool) tool.BaseTool {
	tool, err := utils.InferTool("csv_export",
		"Exports a JSON array of objects to a CSV file in the workspace.",
		func(ctx context.Context, in csvExportInput) (string, error) {
			return toResult(t.Execute(ctx, map[string]interface{}{
				"data":      in.Data,
				"path":      in.Path,
				"delimiter": in.Delimiter,
			}))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt csv_export", "err", err)
		return nil
	}
	return tool
}

// ── helpers ────────────────────────────────────────────────────────────────

// structToMap converts a typed input struct to map[string]interface{} for
// tools that accept a generic args map.
func structToMap(v interface{}) map[string]interface{} {
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]interface{}{"error": err.Error()}
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]interface{}{"error": err.Error()}
	}
	return m
}

// ── business: whatsapp data ────────────────────────────────────────────────

type whatsappParseChatInput struct {
	FilePath    string `json:"file_path" jsonschema:"description=Path to the WhatsApp exported chat .txt file"`
	Action      string `json:"action" jsonschema:"description=What to do: parse (extract messages), stats (summary statistics), search (find messages), participants (list participants), timeline (date-based activity)"`
	Query       string `json:"query,omitempty" jsonschema:"description=Search query text (for search action)"`
	Participant string `json:"participant,omitempty" jsonschema:"description=Filter by participant name"`
	DateFrom    string `json:"date_from,omitempty" jsonschema:"description=Start date filter (YYYY-MM-DD)"`
	DateTo      string `json:"date_to,omitempty" jsonschema:"description=End date filter (YYYY-MM-DD)"`
	Limit       int    `json:"limit,omitempty" jsonschema:"description=Max messages to return. Default: 100"`
}

func adaptWhatsAppData(t *tools.WhatsAppData) tool.BaseTool {
	tl, err := utils.InferTool("whatsapp_parse_chat",
		"Parse an exported WhatsApp chat .txt file. Extracts messages, participants, dates, media references, and statistics.",
		func(ctx context.Context, in whatsappParseChatInput) (string, error) {
			return toResult(t.Execute(ctx, structToMap(in)))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt whatsapp_parse_chat", "err", err)
		return nil
	}
	return tl
}

// ── business: browser open ─────────────────────────────────────────────────

type browserOpenInput struct {
	URL string `json:"url" jsonschema:"description=URL to open in the system's default web browser"`
}

func adaptBrowserOpen(t tools.Tool) tool.BaseTool {
	tl, err := utils.InferTool("browser_open",
		"Opens a URL in the system's default web browser. Does not return page content — use the browser tool for content retrieval.",
		func(ctx context.Context, in browserOpenInput) (string, error) {
			return toResult(t.Execute(ctx, map[string]interface{}{
				"url": in.URL,
			}))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt browser_open", "err", err)
		return nil
	}
	return tl
}

// ── business: image info ───────────────────────────────────────────────────

type imageInfoInput struct {
	Path string `json:"path" jsonschema:"description=Path to the image file relative to workspace root"`
}

func adaptImageInfo(t tools.Tool) tool.BaseTool {
	tl, err := utils.InferTool("image_info",
		"Returns dimensions, format, and file size of an image file in the workspace.",
		func(ctx context.Context, in imageInfoInput) (string, error) {
			return toResult(t.Execute(ctx, map[string]interface{}{
				"path": in.Path,
			}))
		})
	if err != nil {
		slog.Warn("eino: failed to adapt image_info", "err", err)
		return nil
	}
	return tl
}

// ── business tools collector ───────────────────────────────────────────────

func adaptBusinessTools(cfg *ToolConfig) []tool.BaseTool {
	w := cfg.Workspace

	whatsappData := tools.NewWhatsAppData(w)
	browserOpen := tools.NewBrowserOpen()
	imageInfo := tools.NewImageInfo(w)

	adapters := []func() tool.BaseTool{
		func() tool.BaseTool { return adaptWhatsAppData(whatsappData) },
		func() tool.BaseTool { return adaptBrowserOpen(browserOpen) },
		func() tool.BaseTool { return adaptImageInfo(imageInfo) },
	}

	out := make([]tool.BaseTool, 0, len(adapters))
	for _, fn := range adapters {
		t := fn()
		if t != nil {
			out = append(out, t)
		}
	}
	return out
}

// ── memory tool stubs ──────────────────────────────────────────────────────
// The actual memory.Pipeline is wired at runtime. These stubs return
// placeholder messages until the pipeline is configured via ToolConfig.

type memorySearchInput struct {
	Query string `json:"query" jsonschema:"description=Search query"`
}

func adaptMemorySearch(p *memory.Pipeline) tool.BaseTool {
	tl, err := utils.InferTool("memory_search",
		"Search your memory for information about a topic, person, or past conversation.",
		func(ctx context.Context, in memorySearchInput) (string, error) {
			if p == nil {
				return "", fmt.Errorf("memory pipeline not available")
			}
			results, err := p.Search(ctx, in.Query, 5)
			if err != nil {
				return "", fmt.Errorf("memory search failed: %w", err)
			}
			if results.TotalResults() == 0 {
				return "No relevant memories found.", nil
			}
			return results.Formatted(), nil
		})
	if err != nil {
		slog.Warn("eino: failed to adapt memory_search", "err", err)
		return nil
	}
	return tl
}

type memorySaveInput struct {
	Content  string `json:"content" jsonschema:"description=The content to remember"`
	Category string `json:"category,omitempty" jsonschema:"description=Category: fact, preference, rule, or note. Default: note"`
}

func adaptMemorySave(p *memory.Pipeline) tool.BaseTool {
	tl, err := utils.InferTool("memory_save",
		"Save an important fact or piece of information to your memory.",
		func(ctx context.Context, in memorySaveInput) (string, error) {
			if p == nil {
				return "", fmt.Errorf("memory pipeline not available")
			}
			if err := p.IndexContent("agent:"+in.Category, in.Content); err != nil {
				return "", fmt.Errorf("memory save failed: %w", err)
			}
			return "Memory saved successfully.", nil
		})
	if err != nil {
		slog.Warn("eino: failed to adapt memory_save", "err", err)
		return nil
	}
	return tl
}

type memoryRecallInput struct {
	Query string `json:"query" jsonschema:"description=Search query for semantic recall"`
	Limit int    `json:"limit,omitempty" jsonschema:"description=Max results (default: 10, max: 25)"`
}

func adaptMemoryRecall(p *memory.Pipeline) tool.BaseTool {
	tl, err := utils.InferTool("memory_recall",
		"Performs deep semantic recall across all memory.",
		func(ctx context.Context, in memoryRecallInput) (string, error) {
			if p == nil {
				return "", fmt.Errorf("memory pipeline not available")
			}
			limit := in.Limit
			if limit <= 0 || limit > 25 {
				limit = 10
			}
			results, err := p.Search(ctx, in.Query, limit)
			if err != nil {
				return "", fmt.Errorf("memory recall failed: %w", err)
			}
			if results.TotalResults() == 0 {
				return "No memories found.", nil
			}
			return results.Formatted(), nil
		})
	if err != nil {
		slog.Warn("eino: failed to adapt memory_recall", "err", err)
		return nil
	}
	return tl
}

type memoryForgetInput struct {
	Query  string `json:"query" jsonschema:"description=Search query to find the memory to forget"`
	Reason string `json:"reason,omitempty" jsonschema:"description=Why this memory should be forgotten"`
}

func adaptMemoryForget(p *memory.Pipeline) tool.BaseTool {
	tl, err := utils.InferTool("memory_forget",
		"Forgets/removes a specific memory. Use sparingly and only when explicitly asked.",
		func(ctx context.Context, in memoryForgetInput) (string, error) {
			if p == nil {
				return "", fmt.Errorf("memory pipeline not available")
			}
			results, err := p.Search(ctx, in.Query, 3)
			if err != nil {
				return "", fmt.Errorf("memory forget search failed: %w", err)
			}
			if results.TotalResults() == 0 {
				return "No matching memories found to forget.", nil
			}
			return fmt.Sprintf("Found %d matching memories. Forgetting is not yet fully implemented.", results.TotalResults()), nil
		})
	if err != nil {
		slog.Warn("eino: failed to adapt memory_forget", "err", err)
		return nil
	}
	return tl
}

func adaptMemoryTools(cfg *ToolConfig) []tool.BaseTool {
	p := cfg.MemoryPipeline // may be nil — stubs handle this gracefully

	adapters := []func() tool.BaseTool{
		func() tool.BaseTool { return adaptMemorySearch(p) },
		func() tool.BaseTool { return adaptMemorySave(p) },
		func() tool.BaseTool { return adaptMemoryRecall(p) },
		func() tool.BaseTool { return adaptMemoryForget(p) },
	}

	out := make([]tool.BaseTool, 0, len(adapters))
	for _, fn := range adapters {
		t := fn()
		if t != nil {
			out = append(out, t)
		}
	}
	return out
}

// ── config tools ───────────────────────────────────────────────────────────

func adaptConfigSnapshot(cfg *config.Config) tool.BaseTool {
	tl, err := utils.InferTool("config_snapshot",
		"Returns a read-only snapshot of the current runtime configuration: agent settings, security tier, workspace path, and memory limits.",
		func(ctx context.Context, _ struct{}) (string, error) {
			if cfg == nil {
				return "No runtime configuration available.", nil
			}
			snapshot := map[string]interface{}{
				"agent": map[string]interface{}{
					"default_model":     cfg.Agent.DefaultModel,
					"max_output_tokens": cfg.Agent.MaxOutputTokens,
					"temperature":       cfg.Agent.Temperature,
				},
				"security": map[string]interface{}{
					"tier":           cfg.Security.Tier,
					"workspace_only": cfg.Security.WorkspaceOnly,
				},
				"workspace": cfg.Workspace,
				"memory": map[string]interface{}{
					"max_chunk_size": cfg.Memory.MaxChunkSize,
					"retention_days": cfg.Memory.RetentionDays,
				},
				"tools": map[string]interface{}{
					"max_output_bytes": cfg.Tools.Shell.MaxOutputBytes,
				},
			}
			b, err := json.MarshalIndent(snapshot, "", "  ")
			if err != nil {
				return "", fmt.Errorf("config_snapshot: marshal: %v", err)
			}
			return string(b), nil
		})
	if err != nil {
		slog.Warn("eino: failed to adapt config_snapshot", "err", err)
		return nil
	}
	return tl
}

func adaptConfigAutonomy(cfg *config.Config) tool.BaseTool {
	tl, err := utils.InferTool("config_autonomy",
		"Returns the current autonomy settings: level, workspace restrictions, rate limits, and cost caps.",
		func(ctx context.Context, _ struct{}) (string, error) {
			if cfg == nil {
				return "No runtime configuration available.", nil
			}
			ac := cfg.Autonomy
			snapshot := map[string]interface{}{
				"level":                      ac.Level,
				"workspace_only":             ac.WorkspaceOnly,
				"allowed_commands":           ac.AllowedCommands,
				"forbidden_paths":            ac.ForbiddenPaths,
				"max_actions_per_hour":       ac.MaxActionsPerHour,
				"max_cost_per_day_cents":     ac.MaxCostPerDayCents,
				"require_task_plan_approval": ac.RequireTaskPlanApproval,
				"block_high_risk_commands":   ac.BlockHighRiskCommands,
			}
			b, err := json.MarshalIndent(snapshot, "", "  ")
			if err != nil {
				return "", fmt.Errorf("config_autonomy: marshal: %v", err)
			}
			return string(b), nil
		})
	if err != nil {
		slog.Warn("eino: failed to adapt config_autonomy", "err", err)
		return nil
	}
	return tl
}

func adaptConfigDataPaths(cfg *config.Config) tool.BaseTool {
	tl, err := utils.InferTool("config_data_paths",
		"Returns the filesystem paths the agent can use (workspace root, action directory).",
		func(ctx context.Context, _ struct{}) (string, error) {
			if cfg == nil {
				return "No runtime configuration available.", nil
			}
			paths := map[string]interface{}{
				"workspace":  cfg.Workspace,
				"action_dir": cfg.Workspace,
			}
			b, err := json.MarshalIndent(paths, "", "  ")
			if err != nil {
				return "", fmt.Errorf("config_data_paths: marshal: %v", err)
			}
			return string(b), nil
		})
	if err != nil {
		slog.Warn("eino: failed to adapt config_data_paths", "err", err)
		return nil
	}
	return tl
}

func adaptConfigTools(cfg *ToolConfig) []tool.BaseTool {
	if cfg.RuntimeConfig == nil {
		return nil
	}

	c := cfg.RuntimeConfig

	adapters := []func() tool.BaseTool{
		func() tool.BaseTool { return adaptConfigSnapshot(c) },
		func() tool.BaseTool { return adaptConfigAutonomy(c) },
		func() tool.BaseTool { return adaptConfigDataPaths(c) },
	}

	out := make([]tool.BaseTool, 0, len(adapters))
	for _, fn := range adapters {
		t := fn()
		if t != nil {
			out = append(out, t)
		}
	}
	return out
}
