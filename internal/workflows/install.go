package workflows

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// InstallFromURL fetches a SKILL.md from a URL and installs it as a workflow.
// Returns the installed workflow directory path.
func InstallFromURL(workspaceDir, url string, timeout time.Duration) (string, error) {
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	// Basic URL validation.
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return "", fmt.Errorf("install: URL must start with https:// or http://")
	}
	if !strings.HasSuffix(strings.ToLower(url), ".md") {
		return "", fmt.Errorf("install: URL must point to a .md file (SKILL.md)")
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("install: fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("install: HTTP %d from %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB cap
	if err != nil {
		return "", fmt.Errorf("install: read body: %w", err)
	}

	content := string(body)
	name := extractWorkflowName(content)
	if err := validateWorkflowFrontmatter(content); err != nil {
		return "", fmt.Errorf("install: invalid SKILL.md: %w", err)
	}
	if name == "" {
		name = sanitizeWorkflowName(filepath.Base(url))
	}

	dirName := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	dirPath := filepath.Join(workspaceDir, dirName)

	// Reject collisions.
	if _, err := os.Stat(dirPath); err == nil {
		return "", fmt.Errorf("install: workflow %q already exists at %s", name, dirPath)
	}

	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return "", fmt.Errorf("install: create dir: %w", err)
	}

	outPath := filepath.Join(dirPath, "SKILL.md")
	if err := os.WriteFile(outPath, body, 0644); err != nil {
		return "", fmt.Errorf("install: write SKILL.md: %w", err)
	}

	return dirPath, nil
}

// Uninstall removes a user-scope workflow directory.
func Uninstall(userWorkflowsDir, name string) error {
	dirName := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	// Sanitize: reject empty name, path traversal, and special dirs.
	if dirName == "" || dirName == "." || dirName == ".." ||
		strings.Contains(dirName, "/") || strings.Contains(dirName, "\\") ||
		strings.Contains(dirName, "..") {
		return fmt.Errorf("uninstall: invalid workflow name %q", name)
	}
	dirPath := filepath.Join(userWorkflowsDir, dirName)

	// Verify the resolved path is within userWorkflowsDir.
	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		return fmt.Errorf("uninstall: resolve path: %w", err)
	}
	absBase, err := filepath.Abs(userWorkflowsDir)
	if err != nil {
		return fmt.Errorf("uninstall: resolve base: %w", err)
	}
	if !strings.HasPrefix(absPath, absBase+string(filepath.Separator)) && absPath != absBase {
		return fmt.Errorf("uninstall: path escapes workflows directory")
	}

	info, err := os.Stat(dirPath)
	if err != nil {
		return fmt.Errorf("uninstall: workflow %q not found", name)
	}
	if !info.IsDir() {
		return fmt.Errorf("uninstall: %q is not a directory", name)
	}

	return os.RemoveAll(dirPath)
}

// localExtractFrontmatter splits YAML frontmatter from body for install.go use.
func localExtractFrontmatter(content string) (string, string) {
	lines := strings.Split(content, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return "", content
	}
	var endIdx int
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			endIdx = i
			break
		}
	}
	if endIdx == 0 {
		return "", content
	}
	return strings.Join(lines[1:endIdx], "\n"), strings.Join(lines[endIdx+1:], "\n")
}

func extractWorkflowName(content string) string {
	// Try YAML frontmatter first.
	if fm, _ := localExtractFrontmatter(content); fm != "" {
		for _, line := range strings.Split(fm, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "name:") {
				name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "name:"))
				name = strings.Trim(name, "\"'")
				if name != "" {
					return name
				}
			}
		}
	}
	// Fall back to first markdown heading.
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimPrefix(trimmed, "# ")
		}
	}
	return ""
}

// validateWorkflowFrontmatter checks that the SKILL.md has valid YAML frontmatter
// with required fields. Returns nil on success or a descriptive error.
func validateWorkflowFrontmatter(content string) error {
	fm, body := localExtractFrontmatter(content)
	if fm == "" {
		if strings.TrimSpace(body) == "" {
			return fmt.Errorf("SKILL.md is empty")
		}
		return nil
	}

	hasName := false
	hasDescription := false
	for _, line := range strings.Split(fm, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "name:") {
			hasName = true
		}
		if strings.HasPrefix(trimmed, "description:") {
			hasDescription = true
		}
	}

	if !hasName {
		return fmt.Errorf("frontmatter missing required field: name")
	}
	if !hasDescription {
		return fmt.Errorf("frontmatter missing required field: description")
	}
	return nil
}
func sanitizeWorkflowName(filename string) string {
	name := strings.TrimSuffix(filename, ".md")
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, name)
	if name == "" {
		return "installed-workflow"
	}
	return strings.Trim(name, "-")
}
