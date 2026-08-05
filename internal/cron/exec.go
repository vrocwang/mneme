package cron

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

// ExecuteShell runs a shell command and returns its combined output.
func ExecuteShell(ctx context.Context, command string) (string, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.CommandContext(ctx, "powershell", "-Command", command)
	default:
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("shell: %w\n%s", err, output)
	}
	return string(output), nil
}
