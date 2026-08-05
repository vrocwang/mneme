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
)

type manifest struct {
	Name   string `json:"name"`
	Binary string `json:"binary"`
}

func main() {
	// Derive project root from this source file's location, so the tool
	// works regardless of the current working directory (important when
	// invoked as a Wails prebuild hook).
	_, srcFile, _, _ := runtime.Caller(0)
	projectRoot := filepath.Dir(filepath.Dir(filepath.Dir(srcFile)))
	extDir := filepath.Join(projectRoot, "extensions")
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

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		srcDir := filepath.Join(extDir, e.Name())
		manifestPath := filepath.Join(srcDir, "manifest.json")
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
			os.WriteFile(filepath.Join(dstDir, "manifest.json"), mfData, 0644)
		}

		if targetOS != "windows" {
			os.Chmod(out, 0755)
		}

		fmt.Printf("    OK\n")
		built++
	}

	fmt.Printf("\n=== Done: %d built, %d skipped, %d failed ===\n", built, skipped, failed)
	if built > 0 {
		fmt.Println("\nExtensions compiled. Now run: wails build")
	}
	if failed > 0 {
		os.Exit(1)
	}
}
