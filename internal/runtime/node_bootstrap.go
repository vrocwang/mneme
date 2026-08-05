package runtime

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// NodeDistribution describes a Node.js release available for download.
type NodeDistribution struct {
	Version  string `json:"version"` // e.g. "22.11.0"
	URL      string `json:"url"`
	Filename string `json:"filename"`
	SHA256   string `json:"sha256"`
	Platform string `json:"platform"`
	Arch     string `json:"arch"`
}

// ResolvedNode describes a resolved Node.js installation.
type ResolvedNode struct {
	Version *ParsedVersion `json:"version"`
	Binary  string         `json:"binary"`
	Source  string         `json:"source"` // "system", "managed"
	NpmPath string         `json:"npm_path,omitempty"`
}

// SystemNode resolves the system-installed Node.js.
func SystemNode() (*ResolvedNode, error) {
	path, err := resolveBinary("node")
	if err != nil {
		return nil, err
	}
	version, err := probeVersion(path, "--version")
	if err != nil {
		return nil, err
	}
	parsed, err := ParseVersion(version)
	if err != nil {
		return nil, fmt.Errorf("parse node version %q: %w", version, err)
	}
	rn := &ResolvedNode{
		Version: parsed,
		Binary:  path,
		Source:  "system",
	}
	if npmPath, err := resolveBinary("npm"); err == nil {
		rn.NpmPath = npmPath
	}
	return rn, nil
}

// FetchNodeDistribution returns the Node.js distribution metadata for a given version.
func FetchNodeDistribution(version string) (*NodeDistribution, error) {
	v := strings.TrimPrefix(version, "v")
	platform := runtime.GOOS
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x64"
	} else if arch == "arm64" {
		arch = "arm64"
	}

	var filename, url string
	switch platform {
	case "linux":
		filename = fmt.Sprintf("node-v%s-linux-%s.tar.xz", v, arch)
		url = fmt.Sprintf("https://nodejs.org/dist/v%s/%s", v, filename)
	case "darwin":
		filename = fmt.Sprintf("node-v%s-darwin-%s.tar.gz", v, arch)
		url = fmt.Sprintf("https://nodejs.org/dist/v%s/%s", v, filename)
	case "windows":
		filename = fmt.Sprintf("node-v%s-win-%s.zip", v, arch)
		url = fmt.Sprintf("https://nodejs.org/dist/v%s/%s", v, filename)
	default:
		return nil, fmt.Errorf("unsupported platform: %s", platform)
	}

	return &NodeDistribution{
		Version:  v,
		URL:      url,
		Filename: filename,
		Platform: platform,
		Arch:     arch,
	}, nil
}

// FetchNodeSHASums retrieves the SHASUMS256.txt for a Node.js version.
func FetchNodeSHASums(version string) (map[string]string, error) {
	v := strings.TrimPrefix(version, "v")
	url := fmt.Sprintf("https://nodejs.org/dist/v%s/SHASUMS256.txt", v)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch SHASUMS256: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch SHASUMS256: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read SHASUMS256: %w", err)
	}

	sums := make(map[string]string)
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) == 2 {
			sums[strings.TrimSpace(parts[1])] = strings.TrimSpace(parts[0])
		}
	}
	return sums, nil
}

// DownloadNodeDistribution downloads the Node.js distribution to a temp directory.
func DownloadNodeDistribution(ctx context.Context, dist *NodeDistribution, destDir string) (string, error) {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("create dest dir: %w", err)
	}

	destPath := filepath.Join(destDir, dist.Filename)

	// Skip if already downloaded and verified.
	if info, err := os.Stat(destPath); err == nil && info.Size() > 0 {
		return destPath, nil
	}

	client := &http.Client{Timeout: 10 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, "GET", dist.URL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", dist.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download: HTTP %d from %s", resp.StatusCode, dist.URL)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("create file %s: %w", destPath, err)
	}
	defer f.Close()

	_, err = io.Copy(f, io.LimitReader(resp.Body, 100<<20)) // 100MB cap
	if err != nil {
		os.Remove(destPath)
		return "", fmt.Errorf("download write: %w", err)
	}

	return destPath, nil
}

// VerifyNodeSHASum verifies the downloaded archive against the expected SHA256.
func VerifyNodeSHASum(filePath string, expectedSHA string) error {
	if expectedSHA == "" {
		return nil // no checksum to verify
	}
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open for verify: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash: %w", err)
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if actual != strings.ToLower(expectedSHA) {
		return fmt.Errorf("SHA256 mismatch: expected %s, got %s", expectedSHA, actual)
	}
	return nil
}

// ExtractNodeDistribution extracts the Node.js archive to destDir.
func ExtractNodeDistribution(archivePath, destDir string) (string, error) {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("create extract dir: %w", err)
	}

	switch {
	case strings.HasSuffix(archivePath, ".tar.xz"):
		return extractTarXZ(archivePath, destDir)
	case strings.HasSuffix(archivePath, ".tar.gz"):
		return extractTarGZ(archivePath, destDir)
	case strings.HasSuffix(archivePath, ".zip"):
		return extractZip(archivePath, destDir)
	default:
		return "", fmt.Errorf("unsupported archive format: %s", filepath.Base(archivePath))
	}
}

// AtomicInstallNode installs a Node.js distribution atomically.
// Returns the path to the node binary.
func AtomicInstallNode(extractedDir, installDir string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(installDir), 0755); err != nil {
		return "", fmt.Errorf("create parent dir: %w", err)
	}

	// Find the top-level directory inside the extracted archive
	// (e.g. node-v22.11.0-linux-x64/).
	entries, err := os.ReadDir(extractedDir)
	if err != nil {
		return "", fmt.Errorf("read extracted dir: %w", err)
	}

	var topDir string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "node-v") {
			topDir = filepath.Join(extractedDir, e.Name())
			break
		}
	}
	if topDir == "" {
		return "", fmt.Errorf("node distribution directory not found in %s", extractedDir)
	}

	// Remove existing install if present.
	if _, err := os.Stat(installDir); err == nil {
		if err := os.RemoveAll(installDir); err != nil {
			return "", fmt.Errorf("remove existing install: %w", err)
		}
	}

	// Atomic rename (works within same filesystem). Falls back to copy+delete
	// for cross-filesystem moves.
	if err := os.Rename(topDir, installDir); err != nil {
		if linkErr, ok := err.(*os.LinkError); ok && isCrossDevice(linkErr) {
			if err := copyDir(topDir, installDir); err != nil {
				return "", fmt.Errorf("cross-device install: %w", err)
			}
			os.RemoveAll(topDir)
		} else {
			return "", fmt.Errorf("atomic install: %w", err)
		}
	}

	// Find the node binary.
	nodeBin := filepath.Join(installDir, "bin", "node")
	if runtime.GOOS == "windows" {
		nodeBin = filepath.Join(installDir, "node.exe")
	}
	if _, err := os.Stat(nodeBin); err != nil {
		return "", fmt.Errorf("node binary not found at %s: %w", nodeBin, err)
	}

	// Make binary executable.
	os.Chmod(nodeBin, 0755)

	return nodeBin, nil
}

// ── Archive extractors ──────────────────────────────────────────────

func extractTarXZ(archivePath, destDir string) (string, error) {
	// Shell out to system tar for xz decompression. Node.js Linux binaries
	// ship as .tar.xz. macOS uses .tar.gz (handled by extractTarGZ).
	cmd := exec.Command("tar", "-xJf", archivePath, "-C", destDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tar extract: %w\n%s", err, out)
	}
	return destDir, nil
}

func extractTarGZ(archivePath, destDir string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("tar read: %w", err)
		}

		target := filepath.Join(destDir, hdr.Name)
		// Prevent path traversal.
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)+string(filepath.Separator)) {
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, 0755)
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(target), 0755)
			out, err := os.Create(target)
			if err != nil {
				return "", err
			}
			io.Copy(out, io.LimitReader(tr, 1<<30))
			out.Close()
			os.Chmod(target, os.FileMode(hdr.Mode))
		case tar.TypeSymlink:
			os.MkdirAll(filepath.Dir(target), 0755)
			os.Symlink(hdr.Linkname, target)
		}
	}
	return destDir, nil
}

func extractZip(archivePath, destDir string) (string, error) {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", err
	}
	defer r.Close()

	for _, f := range r.File {
		target := filepath.Join(destDir, f.Name)
		// Prevent path traversal.
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)+string(filepath.Separator)) {
			continue
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0755)
		} else {
			os.MkdirAll(filepath.Dir(target), 0755)
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			out, err := os.Create(target)
			if err != nil {
				rc.Close()
				return "", err
			}
			io.Copy(out, io.LimitReader(rc, 1<<30))
			out.Close()
			rc.Close()
		}
	}
	return destDir, nil
}

func resolveBinary(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s not found on PATH", name)
	}
	return path, nil
}

// isCrossDevice checks if the error is caused by a cross-device rename.
func isCrossDevice(err *os.LinkError) bool {
	// os.Rename across devices returns EXDEV on unix.
	return strings.Contains(err.Error(), "cross-device") ||
		strings.Contains(err.Error(), "invalid cross-device")
}

// copyDir recursively copies a directory from src to dst.
func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// copyFile copies a single file from src to dst, preserving permissions.
func copyFile(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()

	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()

	if _, err := io.Copy(d, s); err != nil {
		return err
	}

	info, err := s.Stat()
	if err == nil {
		os.Chmod(dst, info.Mode())
	}
	return nil
}

func probeVersion(binary, flag string) (string, error) {
	cmd := exec.Command(binary, flag)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("version probe: %w\n%s", err, string(out))
	}
	// Some tools write version to stderr (Python). CombinedOutput captures both.
	v := strings.TrimSpace(string(out))
	// Strip common stderr prefixes like "Python 3.12.3\n" (the whole thing is version info).
	if v == "" {
		return "", fmt.Errorf("empty version output from %s %s", binary, flag)
	}
	return v, nil
}
