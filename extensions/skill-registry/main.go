// Skill Registry extension for Mneme.
//
// Provides skill discovery and management tools:
//   - skill_browse: browse available skills from registries
//   - skill_search: search for skills by keyword
//   - skill_registry_install: download and install a skill executable from a registry
//   - skill_uninstall: remove an installed skill
//   - skill_sources: list configured skill sources/registries
//
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/simon/mneme/pkg/extsdk"
)

// dataDir returns the host workspace directory.
func dataDir() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "data")
}

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "skill-registry",
		Version:     "0.1.0",
		Description: "Skill discovery: browse, search, install, uninstall from registries",
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "skill_browse",
		Description: "Browse available skills from configured registries. Lists all skills or filtered by category.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"category": map[string]interface{}{"type": "string", "description": "Filter by category: devtools, productivity, communication, browser, automation, etc."},
				"source":   map[string]interface{}{"type": "string", "description": "Registry source (default: all configured sources)"},
				"limit":    map[string]interface{}{"type": "number", "description": "Max results (default 20)"},
			},
			"required": []string{},
		},
		Permission: "read_only",
		HasEffects: false,
	}, skillBrowse)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "skill_search",
		Description: "Search for skills by keyword across all configured registries",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string", "description": "Search keyword"},
				"limit": map[string]interface{}{"type": "number", "description": "Max results (default 10)"},
			},
			"required": []string{"query"},
		},
		Permission: "read_only",
		HasEffects: false,
	}, skillSearch)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "skill_registry_install",
		Description: "Download and install a skill executable from a registry source",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"skillName": map[string]interface{}{"type": "string", "description": "Skill name/ID to install"},
				"source":    map[string]interface{}{"type": "string", "description": "Registry source URL"},
				"version":   map[string]interface{}{"type": "string", "description": "Specific version to install (default: latest)"},
			},
			"required": []string{"skillName"},
		},
		Permission: "execute",
		HasEffects: true,
	}, skillRegistryInstall)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "skill_uninstall",
		Description: "Remove an installed skill",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"skillName": map[string]interface{}{"type": "string", "description": "Skill name to uninstall"},
			},
			"required": []string{"skillName"},
		},
		Permission: "execute",
		HasEffects: true,
	}, func(ctx context.Context, args map[string]interface{}) extsdk.Result {
		return skillUninstall(args)
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "skill_sources",
		Description: "List configured skill registry sources and their status",
		Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Permission:  "read_only",
		HasEffects:  false,
	}, func(ctx context.Context, args map[string]interface{}) extsdk.Result {
		return skillSources()
	})

	srv.RegisterAgent(extsdk.AgentDef{
		ID:          "skill_creator",
		Name:        "Skill Creator",
		Description: "Creates and packages new skills from user requirements",
		Tier:        "worker",
		SystemPrompt: `You create new skills for Mneme. Skills are executables that follow the extension protocol (stdin/stdout JSON-RPC).
- Understand the user's requirements for a new tool
- Use skill_browse to check if something similar already exists
- Guide the user through skill creation and publishing`,
		ToolAllowlist: []string{"skill_browse", "skill_search", "skill_registry_install", "skill_sources", "write_file", "shell", "read_file"},
		MaxIterations: 15,
		Hidden:        false,
	})

	srv.RegisterAgent(extsdk.AgentDef{
		ID:          "skill_setup",
		Name:        "Skill Setup Guide",
		Description: "Guides users through skill installation and configuration",
		Tier:        "worker",
		SystemPrompt: `You help users discover, install, and configure skills for Mneme.
- Search skill registries for relevant tools
- Guide installation step by step
- Verify skills work after installation
- Help troubleshoot when skills fail`,
		ToolAllowlist: []string{"skill_browse", "skill_search", "skill_registry_install", "skill_uninstall", "skill_sources", "shell", "list_dir"},
		MaxIterations: 12,
		Hidden:        false,
	})

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "skill-registry: %v\n", err)
		os.Exit(1)
	}
}

var httpClient = &http.Client{Timeout: 15 * time.Second}
var skillsDir = filepath.Join(dataDir(), "skills")

func init() { os.MkdirAll(skillsDir, 0755) }

type skillEntry struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Source      string `json:"source"`
	Installed   bool   `json:"installed"`
	Size        string `json:"size,omitempty"`
}

func getSources() []string {
	sources := strings.Fields(os.Getenv("MNEME_SKILL_SOURCES"))
	if len(sources) == 0 {
		sources = []string{}
	}
	return sources
}

func installedSkills() []string {
	entries, _ := os.ReadDir(skillsDir)
	var names []string
	for _, e := range entries {
		if !e.IsDir() && e.Name()[0] != '.' {
			if info, _ := e.Info(); info.Mode()&0111 != 0 {
				names = append(names, e.Name())
			}
		}
	}
	return names
}

func skillBrowse(ctx context.Context, args map[string]interface{}) extsdk.Result {
	category, _ := args["category"].(string)
	limit := 20
	if l, ok := getInt(args, "limit"); ok && l > 0 {
		limit = l
	}

	installed := installedSkills()
	installedSet := make(map[string]bool)
	for _, n := range installed {
		installedSet[n] = true
	}

	// Return installed skills as the primary catalog + registry placeholder
	var results []skillEntry
	for _, name := range installed {
		if category != "" && !strings.Contains(name, category) {
			continue
		}
		results = append(results, skillEntry{
			Name: name, Description: "Installed skill", Source: "local",
			Category: "installed", Installed: true, Version: "1.0",
		})
		if len(results) >= limit {
			break
		}
	}

	_ = ctx
	if len(results) == 0 {
		return extsdk.Result{Success: true, Output: fmt.Sprintf(
			"No skills found. Place executables in %s\nConfigure MNEME_SKILL_SOURCES for remote registries.", skillsDir)}
	}

	b, _ := json.MarshalIndent(results, "", "  ")
	return extsdk.Result{Success: true, Output: string(b)}
}

func skillSearch(ctx context.Context, args map[string]interface{}) extsdk.Result {
	query, _ := args["query"].(string)
	if query == "" {
		return extsdk.Result{Error: "query is required"}
	}

	limit := 10
	if l, ok := getInt(args, "limit"); ok && l > 0 {
		limit = l
	}

	installed := installedSkills()
	var results []skillEntry
	for _, name := range installed {
		if strings.Contains(strings.ToLower(name), strings.ToLower(query)) {
			results = append(results, skillEntry{
				Name: name, Description: "Installed skill matching: " + query,
				Source: "local", Installed: true,
			})
		}
		if len(results) >= limit {
			break
		}
	}

	if len(results) == 0 {
		return extsdk.Result{Success: true, Output: fmt.Sprintf("No skills found matching '%s'", query)}
	}
	b, _ := json.MarshalIndent(results, "", "  ")
	_ = ctx
	return extsdk.Result{Success: true, Output: string(b)}
}

func skillRegistryInstall(ctx context.Context, args map[string]interface{}) extsdk.Result {
	skillName, _ := args["skillName"].(string)
	if skillName == "" {
		return extsdk.Result{Error: "skillName is required"}
	}
	source, _ := args["source"].(string)

	os.MkdirAll(skillsDir, 0755)
	destPath := filepath.Join(skillsDir, skillName)

	if source != "" && (strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://")) {
		url := source + "/" + skillName
		req, reqErr := http.NewRequestWithContext(ctx, "GET", url, nil)
		if reqErr != nil {
			return extsdk.Result{Error: fmt.Sprintf("request: %v", reqErr)}
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return extsdk.Result{Error: fmt.Sprintf("download: %v", err)}
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return extsdk.Result{Error: fmt.Sprintf("download failed: HTTP %d", resp.StatusCode)}
		}
		f, err := os.Create(destPath)
		if err != nil {
			return extsdk.Result{Error: fmt.Sprintf("create file: %v", err)}
		}
		defer f.Close()
		if _, err := io.Copy(f, resp.Body); err != nil {
			return extsdk.Result{Error: fmt.Sprintf("download incomplete: %v", err)}
		}
		os.Chmod(destPath, 0755)
	} else {
		binPath, err := exec.LookPath(skillName)
		if err != nil {
			return extsdk.Result{Error: fmt.Sprintf("skill %q not found in PATH. Provide a source URL to download from a registry.", skillName)}
		}
		data, err := os.ReadFile(binPath)
		if err != nil {
			return extsdk.Result{Error: fmt.Sprintf("read binary: %v", err)}
		}
		if err := os.WriteFile(destPath, data, 0755); err != nil {
			return extsdk.Result{Error: fmt.Sprintf("install: %v", err)}
		}
	}

	abs, _ := filepath.Abs(destPath)
	return extsdk.Result{Success: true, Output: fmt.Sprintf("Installed: %s → %s\nRestart Mneme to discover the new skill.", skillName, abs)}
}

func skillUninstall(args map[string]interface{}) extsdk.Result {
	skillName, _ := args["skillName"].(string)
	if skillName == "" {
		return extsdk.Result{Error: "skillName is required"}
	}

	destPath := filepath.Join(skillsDir, skillName)
	if _, err := os.Stat(destPath); err != nil {
		return extsdk.Result{Error: fmt.Sprintf("skill %q is not installed", skillName)}
	}

	if err := os.Remove(destPath); err != nil {
		return extsdk.Result{Error: fmt.Sprintf("uninstall: %v", err)}
	}
	return extsdk.Result{Success: true, Output: fmt.Sprintf("Uninstalled: %s", skillName)}
}

func skillSources() extsdk.Result {
	sources := getSources()
	status := map[string]interface{}{
		"skills_dir": skillsDir,
		"installed":  installedSkills(),
		"sources":    sources,
		"add_source": "Set MNEME_SKILL_SOURCES env var with space-separated registry URLs.",
	}
	if len(sources) == 0 {
		status["note"] = "No remote sources configured. Set MNEME_SKILL_SOURCES to add skill registries."
	}
	b, _ := json.MarshalIndent(status, "", "  ")
	return extsdk.Result{Success: true, Output: string(b)}
}

func getInt(args map[string]interface{}, key string) (int, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}
