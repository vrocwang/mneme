// Tool Update extension for Mneme.
//
// Provides:
//   - update_check: check current version against latest release
//   - update_apply: download and apply the latest update
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
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/simon/mneme/pkg/extsdk"
)

// dataDir returns the host workspace directory.
func dataDir() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "data")
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

// Current version — can be overridden at build time with ldflags
var currentVersion = "0.1.0"

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "tool-update",
		Version:     "0.1.0",
		Description: "Check for and apply Mneme updates from GitHub releases",
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "update_check",
		Description: "Check current Mneme version against the latest GitHub release",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"repo": map[string]interface{}{"type": "string", "description": "GitHub repo in owner/name format (default: simon/mneme-go)"},
			},
			"required": []string{},
		},
		Permission: "read_only",
		HasEffects: false,
	}, updateCheck)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "update_apply",
		Description: "Download and apply the latest Mneme update from GitHub releases",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"repo":    map[string]interface{}{"type": "string", "description": "GitHub repo in owner/name format (default: simon/mneme-go)"},
				"version": map[string]interface{}{"type": "string", "description": "Specific version to install (default: latest)"},
			},
			"required": []string{},
		},
		Permission: "execute",
		HasEffects: true,
	}, updateApply)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tool-update: %v\n", err)
		os.Exit(1)
	}
}

func getRepo(args map[string]interface{}) string {
	repo, _ := args["repo"].(string)
	if repo == "" {
		repo = "simon/mneme-go"
	}
	return repo
}

func updateCheck(ctx context.Context, args map[string]interface{}) extsdk.Result {
	repo := getRepo(args)
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("request: %v", err)}
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("GitHub API: %v", err)}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return extsdk.Result{Error: fmt.Sprintf("GitHub API error %d: %s", resp.StatusCode, truncate(string(body), 300))}
	}

	var release struct {
		TagName     string `json:"tag_name"`
		Name        string `json:"name"`
		PublishedAt string `json:"published_at"`
		HTMLURL     string `json:"html_url"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return extsdk.Result{Error: fmt.Sprintf("parse: %v", err)}
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	updateAvailable := latestVersion != currentVersion && latestVersion != ""

	result := map[string]interface{}{
		"current":          currentVersion,
		"latest":           latestVersion,
		"release_name":     release.Name,
		"published":        release.PublishedAt,
		"release_url":      release.HTMLURL,
		"update_available": updateAvailable,
		"os":               runtime.GOOS,
		"arch":             runtime.GOARCH,
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return extsdk.Result{Success: true, Output: string(b)}
}

func updateApply(ctx context.Context, args map[string]interface{}) extsdk.Result {
	repo := getRepo(args)
	targetVersion, _ := args["version"].(string)

	// Determine the asset name based on OS/arch
	assetSuffix := ""
	switch runtime.GOOS {
	case "linux":
		assetSuffix = "linux_amd64"
	case "darwin":
		assetSuffix = "darwin_amd64"
	default:
		return extsdk.Result{Error: fmt.Sprintf("unsupported OS: %s", runtime.GOOS)}
	}

	if runtime.GOARCH == "arm64" {
		assetSuffix = strings.Replace(assetSuffix, "amd64", "arm64", 1)
	}

	// Get latest release info
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	if targetVersion != "" {
		ver := strings.TrimPrefix(targetVersion, "v")
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/v%s", repo, ver)
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("GitHub API: %v", err)}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return extsdk.Result{Error: fmt.Sprintf("GitHub API error %d", resp.StatusCode)}
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return extsdk.Result{Error: fmt.Sprintf("parse: %v", err)}
	}

	// Find matching asset
	var downloadURL string
	for _, a := range release.Assets {
		if strings.Contains(a.Name, assetSuffix) {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		return extsdk.Result{Error: fmt.Sprintf("no asset found for %s (available: %d assets)", assetSuffix, len(release.Assets))}
	}

	// Download the asset
	downloadDir := filepath.Join(dataDir(), "updates")
	os.MkdirAll(downloadDir, 0755)
	destPath := filepath.Join(downloadDir, "mneme-"+release.TagName)

	dlReq, reqErr := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if reqErr != nil {
		return extsdk.Result{Error: fmt.Sprintf("create download request: %v", reqErr)}
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		dlReq.Header.Set("Authorization", "Bearer "+token)
	}
	dlResp, err := httpClient.Do(dlReq)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("download: %v", err)}
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode >= 400 {
		return extsdk.Result{Error: fmt.Sprintf("download failed: HTTP %d", dlResp.StatusCode)}
	}

	f, err := os.Create(destPath)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("create file: %v", err)}
	}
	defer f.Close()

	written, copyErr := io.Copy(f, dlResp.Body)
	if copyErr != nil {
		return extsdk.Result{Error: fmt.Sprintf("download incomplete: %v", copyErr)}
	}
	os.Chmod(destPath, 0755)

	// Verify downloaded binary is non-empty.
	data, readErr := os.ReadFile(destPath)
	if readErr != nil {
		return extsdk.Result{Error: fmt.Sprintf("read downloaded binary: %v", readErr)}
	}
	if len(data) == 0 {
		return extsdk.Result{Error: "downloaded binary is empty — update aborted"}
	}

	// Try to install: use atomic rename pattern (write to temp, then rename).
	execPath, _ := os.Executable()
	execDir := filepath.Dir(execPath)
	targetPath := filepath.Join(execDir, "mneme-go")

	var installMsg string
	if execPath != "" {
		// Write to temp file first, then rename atomically.
		tmpPath := targetPath + ".tmp"
		if err := os.WriteFile(tmpPath, data, 0755); err != nil {
			installMsg = fmt.Sprintf("Could not write %s: %v. New binary at %s.", tmpPath, err, destPath)
		} else if err := os.Rename(tmpPath, targetPath); err != nil {
			installMsg = fmt.Sprintf("Could not replace %s: %v. New binary at %s.", targetPath, err, destPath)
		} else {
			installMsg = fmt.Sprintf("Updated: %s -> %s (%d bytes)", execPath, targetPath, written)
		}
	} else {
		installMsg = fmt.Sprintf("Downloaded to %s (%d bytes). Replace your current binary manually.", destPath, written)
	}

	result := map[string]interface{}{
		"version":  release.TagName,
		"asset":    assetSuffix,
		"download": downloadURL,
		"status":   installMsg,
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return extsdk.Result{Success: true, Output: string(b)}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "\n...[truncated]"
	}
	return s
}
