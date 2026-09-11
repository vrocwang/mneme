package workspace

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

// Identity file names created at workspace bootstrap. Shared by the soul
// editor, subconscious evaluators, and the sandbox blocklist so the same
// names never drift between subsystems.
const (
	FileSoul      = "SOUL.md"
	FileIdentity  = "IDENTITY.md"
	FileHeartbeat = "HEARTBEAT.md"
	FileProfile   = "PROFILE.md"
)

// Workspace subdirectory names. These are the single source of truth for the
// on-disk layout: callers use filepath.Join(root, workspace.DirX) (or the Path
// helpers below) instead of repeating literals, so renaming a directory is a
// one-line change here.
const (
	DirAgents       = "agents"            // user-defined agent TOML definitions
	DirTaskBoards   = "agent_task_boards" // agent kanban task boards
	DirConfig       = "config"            // extension config overrides
	DirData         = "data"              // SQLite DB, encryption keys, master salt
	DirExtensions   = "extensions"        // MCP & tool extensions
	DirLogs         = "logs"              // application logs
	DirMemory       = "memory"            // memory tree, entity registry
	DirProjects     = "projects"          // agent sandbox workspace
	DirPrompts      = "prompts"           // custom prompt overrides
	DirScreenshots  = "screenshots"       // desktop screen captures
	DirSecrets      = "secrets"           // keyring encrypted secrets
	DirSkills       = "skills"            // user-installed skill definitions
	DirTmp          = "tmp"               // temporary files
	DirTranscripts  = "transcripts"       // session transcripts
	DirWorkflowLogs = "workflow-logs"     // workflow execution logs
	DirWorkflows    = "workflows"         // user-defined WORKFLOW.md files
)

// Path helpers return the absolute path of a workspace subdirectory.
func AgentsDir(root string) string       { return filepath.Join(root, DirAgents) }
func TaskBoardsDir(root string) string   { return filepath.Join(root, DirTaskBoards) }
func ConfigDir(root string) string       { return filepath.Join(root, DirConfig) }
func DataDir(root string) string         { return filepath.Join(root, DirData) }
func ExtensionsDir(root string) string   { return filepath.Join(root, DirExtensions) }
func LogsDir(root string) string         { return filepath.Join(root, DirLogs) }
func MemoryDir(root string) string       { return filepath.Join(root, DirMemory) }
func ProjectsDir(root string) string     { return filepath.Join(root, DirProjects) }
func PromptsDir(root string) string      { return filepath.Join(root, DirPrompts) }
func ScreenshotsDir(root string) string  { return filepath.Join(root, DirScreenshots) }
func SecretsDir(root string) string      { return filepath.Join(root, DirSecrets) }
func SkillsDir(root string) string       { return filepath.Join(root, DirSkills) }
func TmpDir(root string) string          { return filepath.Join(root, DirTmp) }
func WorkflowLogsDir(root string) string { return filepath.Join(root, DirWorkflowLogs) }
func WorkflowsDir(root string) string    { return filepath.Join(root, DirWorkflows) }

var layout = []string{
	DirAgents,
	DirTaskBoards,
	DirConfig,
	DirData,
	DirExtensions,
	DirLogs,
	DirMemory,
	DirProjects,
	DirPrompts,
	DirScreenshots,
	DirSecrets,
	DirSkills,
	DirTmp,
	DirTranscripts,
	DirWorkflowLogs,
	DirWorkflows,
}

// bundledFiles defines files created on first workspace bootstrap if they don't
// already exist. These provide the agent with its core identity and operating
// instructions — matching Rust workspace/ops.rs bundled_default_contents.
var bundledFiles = map[string]string{
	FileSoul: `# Mneme Soul

You are Mneme, a personal AI assistant. You are helpful, concise, and
precise. You operate in the user's best interest and respect their autonomy.

## Operating Principles

- Be direct and actionable — prefer concrete answers over vague suggestions.
- When you don't know, say so. Never fabricate information.
- Respect the user's tools, filesystem, and privacy.
- Execute tasks efficiently: plan, act, verify.
`,
	FileIdentity: `# Mneme Identity

This file defines your persistent identity and preferences. The user can edit
it to customize how you behave, what you know about them, and how you should
approach tasks.

## About the User
<!-- Add details about yourself that you want Mneme to remember. -->

## Preferences
<!-- Communication style, tools, workflows you prefer. -->

## Projects
<!-- Active projects and context. -->
`,
}

// ExtensionSources is set by main.go to the embedded extensions FS from the
// production build. HasExtensions is true only when extensions are embedded
// (production build). In dev mode (wails dev) it remains false.
var (
	ExtensionSources embed.FS
	HasExtensions    bool
)

func Bootstrap(root string) error {
	for _, dir := range layout {
		path := filepath.Join(root, dir)
		if err := os.MkdirAll(path, 0755); err != nil {
			return err
		}
	}
	// Seed bundled files if they don't already exist.
	for name, content := range bundledFiles {
		path := filepath.Join(root, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				return err
			}
		}
	}
	return nil
}

// ExtractEmbeddedExtensions writes embedded extension sources into the
// workspace extensions directory if HasExtensions is true (production build).
// In dev mode it's false and extensions are loaded from the project source tree.
func ExtractEmbeddedExtensions(root string) error {
	if !HasExtensions {
		return nil
	}
	extFS := ExtensionSources
	extDir := filepath.Join(root, "extensions")

	// Check if at least one extension subdirectory already exists with a
	// manifest. If so, skip extraction — the user may have installed or
	// updated extensions manually.
	entries, err := fs.ReadDir(extFS, "extensions-dist")
	if err != nil {
		return nil // no extensions embedded (dev build)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dstDir := filepath.Join(extDir, entry.Name())
		if _, err := os.Stat(filepath.Join(dstDir, "manifest.json")); err == nil {
			// Already exists — skip all extraction.
			return nil
		}
	}

	// First run: extract all extension directories.
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		srcDir := "extensions-dist/" + entry.Name()
		dstDir := filepath.Join(extDir, entry.Name())
		if err := copyEmbedDir(extFS, srcDir, dstDir); err != nil {
			return err
		}
	}
	return nil
}

func copyEmbedDir(efs embed.FS, src, dst string) error {
	entries, err := fs.ReadDir(efs, src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := src + "/" + entry.Name()
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyEmbedDir(efs, srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		data, err := fs.ReadFile(efs, srcPath)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dstPath, data, 0644); err != nil {
			return err
		}
	}
	return nil
}
