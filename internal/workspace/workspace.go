package workspace

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

var layout = []string{
	"agents",            // user-defined agent TOML definitions
	"agent_task_boards", // agent kanban task boards
	"config",            // extension config overrides
	"data",              // SQLite DB, encryption keys, master salt
	"extensions",        // MCP & tool extensions
	"logs",              // application logs
	"memory",            // memory tree, entity registry
	"projects",          // agent sandbox workspace
	"prompts",           // custom prompt overrides
	"screenshots",       // desktop screen captures
	"secrets",           // keyring encrypted secrets
	"skills",            // user-installed skill definitions
	"tmp",               // temporary files
	"transcripts",       // session transcripts
	"workflow-logs",     // workflow execution logs
	"workflows",         // user-defined WORKFLOW.md files
}

// bundledFiles defines files created on first workspace bootstrap if they don't
// already exist. These provide the agent with its core identity and operating
// instructions — matching Rust workspace/ops.rs bundled_default_contents.
var bundledFiles = map[string]string{
	"SOUL.md": `# Mneme Soul

You are Mneme, a personal AI assistant. You are helpful, concise, and
precise. You operate in the user's best interest and respect their autonomy.

## Operating Principles

- Be direct and actionable — prefer concrete answers over vague suggestions.
- When you don't know, say so. Never fabricate information.
- Respect the user's tools, filesystem, and privacy.
- Execute tasks efficiently: plan, act, verify.
`,
	"IDENTITY.md": `# Mneme Identity

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
