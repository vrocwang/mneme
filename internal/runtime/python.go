package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/simon/mneme/internal/config"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// PythonRuntime manages a Python interpreter and virtual environments.
type PythonRuntime struct {
	mu       sync.Mutex
	venvPath string
	python   string
}

// NewPythonRuntime creates a new Python runtime manager.
// If venvPath is provided, the Python binary from that venv is used.
func NewPythonRuntime(venvPath string) *PythonRuntime {
	python := "python3"
	if runtime.GOOS == "windows" {
		python = "python.exe"
	}
	if _, err := exec.LookPath(python); err != nil {
		if _, err2 := exec.LookPath("python"); err2 == nil {
			python = "python"
		}
	}
	return &PythonRuntime{
		venvPath: venvPath,
		python:   python,
	}
}

// Python returns the path to the Python binary, resolving venv if configured.
func (pr *PythonRuntime) Python() string {
	if pr.venvPath != "" {
		binDir := "bin"
		if runtime.GOOS == "windows" {
			binDir = "Scripts"
		}
		py := filepath.Join(pr.venvPath, binDir, "python3")
		if runtime.GOOS == "windows" {
			py = filepath.Join(pr.venvPath, binDir, "python.exe")
		}
		if _, err := os.Stat(py); err == nil {
			return py
		}
	}
	return pr.python
}

// IsAvailable returns true if Python is installed and accessible.
func (pr *PythonRuntime) IsAvailable() bool {
	_, err := exec.LookPath(pr.Python())
	return err == nil
}

// Version returns the Python version string.
func (pr *PythonRuntime) Version() (string, error) {
	cmd := exec.Command(pr.Python(), "--version")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// RunScript executes a Python script file with optional arguments.
func (pr *PythonRuntime) RunScript(ctx context.Context, scriptPath string, args ...string) (string, error) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	allArgs := append([]string{scriptPath}, args...)
	cmd := exec.CommandContext(ctx, pr.Python(), allArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("python: %w\n%s", err, output)
	}
	return string(output), nil
}

// RunCode executes a Python string by writing it to a temp file and running it.
func (pr *PythonRuntime) RunCode(ctx context.Context, code string, args ...string) (string, error) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	tmpDir := config.TempDir()
	tmpFile, err := os.CreateTemp(tmpDir, "oh-python-*.py")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(code); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("write code: %w", err)
	}
	tmpFile.Close()

	allArgs := append([]string{tmpFile.Name()}, args...)
	cmd := exec.CommandContext(ctx, pr.Python(), allArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("python: %w\n%s", err, output)
	}
	return string(output), nil
}

// CreateVenv creates a Python virtual environment at the given path.
func (pr *PythonRuntime) CreateVenv(ctx context.Context, path string) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("directory already exists: %s", path)
	}

	cmd := exec.CommandContext(ctx, pr.python, "-m", "venv", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("venv create: %w\n%s", err, out)
	}
	return nil
}

// PipInstall installs packages into the configured venv or globally.
func (pr *PythonRuntime) PipInstall(ctx context.Context, packages ...string) (string, error) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	pipPath := "pip3"
	if pr.venvPath != "" {
		binDir := "bin"
		if runtime.GOOS == "windows" {
			binDir = "Scripts"
		}
		pipPath = filepath.Join(pr.venvPath, binDir, "pip3")
	}

	args := append([]string{"install", "--quiet"}, packages...)
	cmd := exec.CommandContext(ctx, pipPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("pip install: %w\n%s", err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

// DetectVenvs scans a directory for Python virtual environments.
func DetectVenvs(searchDir string) ([]string, error) {
	if searchDir == "" {
		searchDir = config.TempDir()
	}
	var venvs []string
	filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return nil
		}
		// A venv has bin/python3 or Scripts/python.exe
		pyBin := filepath.Join(path, "bin", "python3")
		if runtime.GOOS == "windows" {
			pyBin = filepath.Join(path, "Scripts", "python.exe")
		}
		if _, err := os.Stat(pyBin); err == nil {
			venvs = append(venvs, path)
			return filepath.SkipDir
		}
		return nil
	})
	return venvs, nil
}
