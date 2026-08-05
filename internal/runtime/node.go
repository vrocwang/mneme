package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// NodeRuntime manages a Node.js process.
type NodeRuntime struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	workDir string
}

// NewNodeRuntime creates a new Node.js runtime manager.
func NewNodeRuntime(workDir string) *NodeRuntime {
	return &NodeRuntime{workDir: workDir}
}

// RunScript executes a Node.js script with the given args.
// Returns stdout output.
func (n *NodeRuntime) RunScript(ctx context.Context, scriptPath string, args ...string) (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	allArgs := append([]string{scriptPath}, args...)
	cmd := exec.CommandContext(ctx, "node", allArgs...)
	cmd.Dir = n.workDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("node script: %w\n%s", err, output)
	}
	return string(output), nil
}

// StartMCPServer starts an MCP server as a Node.js child process.
// Returns a command that can be used for stdio communication.
func (n *NodeRuntime) StartMCPServer(ctx context.Context, serverPath string) (*exec.Cmd, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	cmd := exec.CommandContext(ctx, "node", serverPath)
	cmd.Dir = n.workDir

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mcp server: %w", err)
	}

	// Wait for startup
	time.Sleep(100 * time.Millisecond)
	return cmd, nil
}

// Stop kills the Node.js process if running.
func (n *NodeRuntime) Stop() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.cmd != nil && n.cmd.Process != nil {
		n.cmd.Process.Kill()
	}
}
