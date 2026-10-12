// Cross-platform extension builder. Compiles every Go extension under
// extensions/ into extensions-dist/ for embedding in the production build.
//
//	Usage: go run cmd/build-extensions/main.go
//
// Cross-compile: GOOS=windows GOARCH=amd64 go run cmd/build-extensions/main.go
//
// This tool is invoked automatically by Wails prebuild hooks so extensions
// are bundled on every release build.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/simon/mneme/internal/workspace"
)

type manifest struct {
	Name   string `json:"name"`
	Binary string `json:"binary"`
}

// agentPackManifest is the marker file that makes a directory an agent pack
// (a data-only bundle of agent.toml + prompt.md, with no Go code and no
// manifest.json). Such directories are copied verbatim instead of compiled.
const agentPackManifest = "agent.toml"

func main() {
	// Derive project root from this source file's location, so the tool
	// works regardless of the current working directory (important when
	// invoked as a Wails prebuild hook).
	_, srcFile, _, _ := runtime.Caller(0)
	projectRoot := filepath.Dir(filepath.Dir(filepath.Dir(srcFile)))
	extDir := filepath.Join(projectRoot, workspace.DirExtensions)
	distDir := filepath.Join(projectRoot, "extensions-dist")

	entries, err := os.ReadDir(extDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read extensions dir %s: %v\n", extDir, err)
		os.Exit(1)
	}

	targetOS := os.Getenv("GOOS")
	if targetOS == "" {
		targetOS = runtime.GOOS
	}
	targetArch := os.Getenv("GOARCH")
	if targetArch == "" {
		targetArch = runtime.GOARCH
	}
	fmt.Printf("Building extensions for %s/%s\n  src:  %s\n  dist: %s\n\n", targetOS, targetArch, extDir, distDir)

	built, skipped, failed := 0, 0, 0
	copied := 0

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		srcDir := filepath.Join(extDir, e.Name())
		manifestPath := filepath.Join(srcDir, workspace.FileExtensionManifest)
		goMod := filepath.Join(srcDir, "go.mod")

		if _, err := os.Stat(goMod); os.IsNotExist(err) {
			continue
		}

		outputName := e.Name()
		if data, err := os.ReadFile(manifestPath); err == nil {
			var mf manifest
			if json.Unmarshal(data, &mf) == nil && mf.Binary != "" {
				outputName = mf.Binary
			}
		}

		dstDir := filepath.Join(distDir, e.Name())
		os.MkdirAll(dstDir, 0755)

		// Clean previous artifacts.
		for _, p := range []string{outputName, outputName + ".exe"} {
			os.Remove(filepath.Join(dstDir, p))
		}

		fmt.Printf("  BUILD %s", e.Name())

		out := filepath.Join(dstDir, outputName)
		if targetOS == "windows" {
			out += ".exe"
		}

		cmd := exec.Command("go", "build",
			"-ldflags", "-s -w",
			"-o", out, ".",
		)
		cmd.Dir = srcDir
		cmd.Env = append(os.Environ(),
			"GOOS="+targetOS,
			"GOARCH="+targetArch,
			"CGO_ENABLED=0",
		)
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout

		if err := cmd.Run(); err != nil {
			fmt.Printf("    FAIL: %v\n", err)
			failed++
			continue
		}

		// Copy manifest.json so the extension is self-contained in dist.
		if mfData, err := os.ReadFile(manifestPath); err == nil {
			os.WriteFile(filepath.Join(dstDir, workspace.FileExtensionManifest), mfData, 0644)
		}

		if targetOS != "windows" {
			os.Chmod(out, 0755)
		}

		fmt.Printf("    OK\n")
		built++
	}

	// Agent packs have no Go code, so the loop above skips them (no go.mod).
	// Ship them anyway: the host loads packs from <workspace>/extensions/agents,
	// which ExtractEmbeddedExtensions populates from extensions-dist/agents.
	packCopied, packFailed := shipAgentPacks(extDir, distDir)
	copied += packCopied
	failed += packFailed

	fmt.Printf("\n=== Done: %d built, %d copied, %d skipped, %d failed ===\n", built, copied, skipped, failed)
	if built > 0 || copied > 0 {
		fmt.Println("\nExtensions compiled. Now run: wails build")
	}
	if failed > 0 {
		os.Exit(1)
	}
}

// shipAgentPacks copies every agent pack under <extDir>/agents into
// <distDir>/agents/<name>/. A pack is a data-only directory (agent.toml +
// prompt.md, no go.mod, no manifest.json) that LoadAgentPacksFromDir reads at
// boot; without this step `extensions-dist/agents/...` is never produced and
// the packs only exist in a development source tree.
//
// Copying is idempotent and additive: each pack's own destination directory is
// rebuilt from scratch, while packs that no longer exist in the source tree are
// left alone (the dist tree is never pruned wholesale).
func shipAgentPacks(extDir, distDir string) (copied, failed int) {
	packsRoot := filepath.Join(extDir, workspace.DirAgents)
	entries, err := os.ReadDir(packsRoot)
	if err != nil {
		return 0, 0 // no agent-packs directory: nothing to ship
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		srcDir := filepath.Join(packsRoot, e.Name())
		if _, err := os.Stat(filepath.Join(srcDir, agentPackManifest)); err != nil {
			continue // not an agent pack
		}

		relName := filepath.Join(workspace.DirAgents, e.Name())
		fmt.Printf("  COPY %s", relName)

		dstDir := filepath.Join(distDir, workspace.DirAgents, e.Name())
		if err := os.RemoveAll(dstDir); err != nil {
			fmt.Printf("    FAIL: %v\n", err)
			failed++
			continue
		}
		if err := copyDir(srcDir, dstDir); err != nil {
			fmt.Printf("    FAIL: %v\n", err)
			failed++
			continue
		}
		fmt.Printf("    OK\n")
		copied++
	}
	return copied, failed
}

// copyDir recursively copies the regular files in src into dst, creating dst and
// any subdirectories. Non-regular entries (symlinks, devices) are skipped: agent
// packs are plain data files.
func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		if !e.Type().IsRegular() {
			continue
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dstPath, data, 0644); err != nil {
			return err
		}
	}
	return nil
}
