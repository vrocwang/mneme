package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// RuntimeKind is the type of runtime.
type RuntimeKind string

const (
	RuntimeNode   RuntimeKind = "node"
	RuntimePython RuntimeKind = "python"
)

// ResolvedRuntime describes a detected runtime installation.
type ResolvedRuntime struct {
	Kind      RuntimeKind `json:"kind"`
	Enabled   bool        `json:"enabled"`
	Available bool        `json:"available"`
	Source    string      `json:"source"` // "system", "managed", "none"
	Version   string      `json:"version"`
	Binary    string      `json:"binary"`
	BinDir    string      `json:"bin_dir,omitempty"`
	Error     string      `json:"error,omitempty"`
}

// ResolvedRuntimeSummary is the result of resolving all runtimes.
type ResolvedRuntimeSummary struct {
	Node   ResolvedRuntime `json:"node"`
	Python ResolvedRuntime `json:"python"`
}

// ResolveRuntimes detects Node and Python installations, reporting
// whether they're available (system-installed or managed), their
// versions, and binary paths. Used before running script-based skills.
func ResolveRuntimes(ctx context.Context) ResolvedRuntimeSummary {
	return ResolvedRuntimeSummary{
		Node:   resolveRuntime(ctx, RuntimeNode),
		Python: resolveRuntime(ctx, RuntimePython),
	}
}

func resolveRuntime(ctx context.Context, kind RuntimeKind) ResolvedRuntime {
	result := ResolvedRuntime{
		Kind:    kind,
		Enabled: true,
		Source:  "none",
	}

	binary := string(kind)
	if kind == RuntimePython {
		// Try python3 first, then python.
		result = probeBinary(ctx, "python3", kind)
		if result.Available {
			return result
		}
	}

	result = probeBinary(ctx, binary, kind)
	return result
}

func probeBinary(ctx context.Context, binary string, kind RuntimeKind) ResolvedRuntime {
	result := ResolvedRuntime{
		Kind:    kind,
		Enabled: true,
		Binary:  binary,
	}

	path, err := exec.LookPath(binary)
	if err != nil {
		result.Source = "none"
		result.Error = fmt.Sprintf("%s not found on PATH", binary)
		return result
	}
	result.Binary = path

	versionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	versionArgs := "--version"
	if kind == RuntimePython {
		versionArgs = "--version"
	}

	cmd := exec.CommandContext(versionCtx, path, versionArgs)
	out, err := cmd.Output()
	if err != nil {
		result.Source = "system"
		result.Available = true
		result.Error = fmt.Sprintf("version check failed: %v", err)
		return result
	}

	result.Version = strings.TrimSpace(string(out))
	result.Available = true

	// Classify source: system-installed or managed.
	if strings.Contains(path, ".mneme") || strings.Contains(path, "managed") {
		result.Source = "managed"
	} else {
		result.Source = "system"
	}

	// Try to get bin directory.
	if kind == RuntimeNode {
		// npm root -g equivalent
		result.BinDir = strings.TrimSuffix(path, "/bin/node")
	}
	if kind == RuntimePython {
		result.BinDir = strings.TrimSuffix(path, "/bin/python3")
	}

	return result
}

// IsAvailable returns true if at least one runtime is available.
func (s ResolvedRuntimeSummary) IsAvailable(kind RuntimeKind) bool {
	switch kind {
	case RuntimeNode:
		return s.Node.Available
	case RuntimePython:
		return s.Python.Available
	default:
		return false
	}
}
