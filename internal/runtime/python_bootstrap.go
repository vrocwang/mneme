package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// PythonDistribution describes a standalone Python build available for download.
type PythonDistribution struct {
	Version  string `json:"version"`
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Platform string `json:"platform"`
	Arch     string `json:"arch"`
}

// ResolvedPython describes a resolved Python installation.
type ResolvedPython struct {
	Version *ParsedVersion `json:"version"`
	Binary  string         `json:"binary"`
	Source  string         `json:"source"` // "system", "managed"
	PipPath string         `json:"pip_path,omitempty"`
}

// SystemPython resolves the system-installed Python.
func SystemPython() (*ResolvedPython, error) {
	for _, name := range []string{"python3", "python"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		version, err := probeVersion(path, "--version")
		if err != nil {
			continue
		}
		parsed, err := ParseVersion(version)
		if err != nil {
			continue
		}
		rp := &ResolvedPython{
			Version: parsed,
			Binary:  path,
			Source:  "system",
		}
		if pipPath, err := exec.LookPath("pip3"); err == nil {
			rp.PipPath = pipPath
		} else if pipPath, err := exec.LookPath("pip"); err == nil {
			rp.PipPath = pipPath
		}
		return rp, nil
	}
	return nil, fmt.Errorf("no Python installation found on PATH")
}

// PythonLaunchSpec describes how to launch a Python script process.
type PythonLaunchSpec struct {
	Binary   string   `json:"binary"` // python binary path
	Args     []string `json:"args"`   // script + extra args
	WorkDir  string   `json:"work_dir,omitempty"`
	Env      []string `json:"env,omitempty"`       // "KEY=VALUE" format
	VenvPath string   `json:"venv_path,omitempty"` // path to virtualenv
	Timeout  int      `json:"timeout_secs,omitempty"`
}

// Command builds an exec.Cmd from the launch spec.
func (s *PythonLaunchSpec) Command(ctx context.Context) *exec.Cmd {
	binary := s.Binary
	if binary == "" {
		binary = "python3"
	}

	// If venv is specified, resolve the python binary inside it.
	if s.VenvPath != "" {
		binDir := "bin"
		if runtime.GOOS == "windows" {
			binDir = "Scripts"
		}
		venvPython := filepath.Join(s.VenvPath, binDir, "python3")
		if runtime.GOOS == "windows" {
			venvPython = filepath.Join(s.VenvPath, binDir, "python.exe")
		}
		if _, err := os.Stat(venvPython); err == nil {
			binary = venvPython
		}
	}

	cmd := exec.CommandContext(ctx, binary, s.Args...)
	if s.WorkDir != "" {
		cmd.Dir = s.WorkDir
	}
	if len(s.Env) > 0 {
		cmd.Env = append(os.Environ(), s.Env...)
	}
	return cmd
}

// Launch executes the Python script defined by the spec and returns stdout.
func (s *PythonLaunchSpec) Launch(ctx context.Context) (string, error) {
	cmd := s.Command(ctx)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("python launch: %w\n%s", err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

// DetectManagedPython checks if a managed Python distribution exists at the given path.
func DetectManagedPython(installDir string) (*ResolvedPython, bool) {
	binDir := "bin"
	if runtime.GOOS == "windows" {
		binDir = "Scripts"
	}
	pyPath := filepath.Join(installDir, binDir, "python3")
	if runtime.GOOS == "windows" {
		pyPath = filepath.Join(installDir, binDir, "python.exe")
	}

	if _, err := os.Stat(pyPath); err != nil {
		return nil, false
	}

	version, err := probeVersion(pyPath, "--version")
	if err != nil {
		return nil, false
	}

	parsed, err := ParseVersion(version)
	if err != nil {
		return nil, false
	}

	rp := &ResolvedPython{
		Version: parsed,
		Binary:  pyPath,
		Source:  "managed",
	}
	if pipPath, err := resolveRelativeBinary(installDir, binDir, "pip3"); err == nil {
		rp.PipPath = pipPath
	}
	return rp, true
}

func resolveRelativeBinary(baseDir, binSubDir, name string) (string, error) {
	path := filepath.Join(baseDir, binSubDir, name)
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return path, nil
}
