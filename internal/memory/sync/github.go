package sync

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitHubRepoConnector syncs a public (or token-authenticated) GitHub repository
// into the memory pipeline. It shallow-clones on first sync and pulls on
// subsequent syncs, walking tree entries filtered by file extensions.
type GitHubRepoConnector struct {
	repoURL    string
	branch     string
	extensions []string
	token      string
	cloneDir   string
	lastCommit string // last known HEAD commit SHA for incremental sync
	sizeLimit  int64  // bytes, default 500KB
}

// NewGitHubRepoConnector creates a connector for a GitHub repository.
// repoURL is the full URL (e.g. "https://github.com/owner/repo").
// branch defaults to "main" if empty. extensions filters file types.
// token is an optional GitHub personal access token for private repos
// and higher rate limits.
func NewGitHubRepoConnector(repoURL, branch string, extensions []string, token string) *GitHubRepoConnector {
	if branch == "" {
		branch = "main"
	}
	return &GitHubRepoConnector{
		repoURL:    repoURL,
		branch:     branch,
		extensions: extensions,
		token:      token,
		cloneDir:   "",         // set lazily via temp dir
		sizeLimit:  500 * 1024, // 500KB
	}
}

func (c *GitHubRepoConnector) Name() string {
	ownerRepo := strings.TrimPrefix(c.repoURL, "https://github.com/")
	ownerRepo = strings.TrimSuffix(ownerRepo, ".git")
	return "github:" + ownerRepo
}

func (c *GitHubRepoConnector) Sync(ctx context.Context) ([]Item, error) {
	// Lazily create clone directory.
	if c.cloneDir == "" {
		dir, err := os.MkdirTemp("", "mneme-github-sync-*")
		if err != nil {
			return nil, fmt.Errorf("create temp clone dir: %w", err)
		}
		c.cloneDir = dir
	}

	repoDir := filepath.Join(c.cloneDir, "repo")

	// Determine clone vs pull.
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); os.IsNotExist(err) {
		if err := c.shallowClone(ctx, repoDir); err != nil {
			return nil, fmt.Errorf("github clone: %w", err)
		}
	} else {
		if err := c.fetchPull(ctx, repoDir); err != nil {
			return nil, fmt.Errorf("github fetch: %w", err)
		}
	}

	// Get current HEAD SHA for incremental tracking.
	headSHA, err := c.revParse(ctx, repoDir, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("github rev-parse HEAD: %w", err)
	}

	// If this is the same commit as last sync, nothing to do.
	if c.lastCommit != "" && c.lastCommit == headSHA {
		return nil, nil
	}

	// Diff from last commit or list all tracked files.
	var files []string
	if c.lastCommit != "" {
		diffFiles, err := c.diffFiles(ctx, repoDir, c.lastCommit, headSHA)
		if err == nil && len(diffFiles) > 0 {
			files = diffFiles
		}
	}
	if len(files) == 0 {
		// Full tree walk for first sync or when diff fails.
		files, err = c.listTrackedFiles(ctx, repoDir)
		if err != nil {
			return nil, fmt.Errorf("github list files: %w", err)
		}
	}

	var items []Item
	for _, relPath := range files {
		select {
		case <-ctx.Done():
			return items, ctx.Err()
		default:
		}

		ext := strings.ToLower(filepath.Ext(relPath))
		if !c.shouldIngest(ext) {
			continue
		}

		absPath := filepath.Join(repoDir, relPath)
		info, err := os.Stat(absPath)
		if err != nil {
			continue
		}
		if info.Size() > c.sizeLimit {
			continue
		}

		data, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}
		if isBinaryContent(data) {
			continue
		}

		items = append(items, Item{
			Source:   c.Name(),
			Path:     relPath,
			Content:  string(data),
			Modified: info.ModTime(),
		})
	}

	c.lastCommit = headSHA
	return items, nil
}

func (c *GitHubRepoConnector) shouldIngest(ext string) bool {
	if len(c.extensions) == 0 {
		return true
	}
	for _, e := range c.extensions {
		if e == ext {
			return true
		}
	}
	return false
}

// shallowClone performs a depth-1 clone of the target branch.
// Uses GIT_ASKPASS via environment variable to avoid exposing the token
// in process command-line arguments (visible in /proc/PID/cmdline).
func (c *GitHubRepoConnector) shallowClone(ctx context.Context, destDir string) error {
	args := []string{"clone", "--depth", "1", "--single-branch", "--branch", c.branch, c.repoURL, destDir}
	cmd := exec.CommandContext(ctx, "git", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if c.token != "" {
		// Pass token via GIT_ASKPASS environment variable — never embed
		// credentials in CLI arguments where they are visible to all local
		// users via /proc/PID/cmdline.
		cmd.Env = append(os.Environ(),
			"GIT_ASKPASS=echo "+c.token,
			"GIT_TERMINAL_PROMPT=0",
		)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone: %w\nstderr: %s", err, stderr.String())
	}
	return nil
}

// fetchPull updates the shallow clone to the latest HEAD.
func (c *GitHubRepoConnector) fetchPull(ctx context.Context, repoDir string) error {
	// Fetch latest.
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "fetch", "--depth", "1", "origin", c.branch)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git fetch: %w\nstderr: %s", err, stderr.String())
	}

	// Reset to fetched HEAD.
	cmd = exec.CommandContext(ctx, "git", "-C", repoDir, "reset", "--hard", "FETCH_HEAD")
	stderr.Reset()
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git reset: %w\nstderr: %s", err, stderr.String())
	}
	return nil
}

// revParse returns the commit SHA for a git ref.
func (c *GitHubRepoConnector) revParse(ctx context.Context, repoDir, ref string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "rev-parse", ref)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git rev-parse: %w\nstderr: %s", err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// listTrackedFiles returns all tracked files in the repository.
func (c *GitHubRepoConnector) listTrackedFiles(ctx context.Context, repoDir string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "ls-files")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git ls-files: %w\nstderr: %s", err, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	var files []string
	for _, l := range lines {
		if l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}

// diffFiles returns files changed between two commits.
func (c *GitHubRepoConnector) diffFiles(ctx context.Context, repoDir, oldSHA, newSHA string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "diff", "--name-only", oldSHA, newSHA)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git diff: %w\nstderr: %s", err, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	var files []string
	for _, l := range lines {
		if l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}

// isBinaryContent uses a simple heuristic to detect binary files.
// It checks for null bytes in the first 8KB of data.
func isBinaryContent(data []byte) bool {
	checkLen := 8192
	if len(data) < checkLen {
		checkLen = len(data)
	}
	for i := 0; i < checkLen; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}

// Ensure interface compliance.
var _ Connector = (*GitHubRepoConnector)(nil)
