// Package sandbox provides sandbox backends for isolating tool execution.
// Supported backends: bubblewrap (Linux), firejail (Linux), and noop (unsupported platforms).
package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Backend is a sandbox implementation that wraps command execution.
type Backend interface {
	// Name returns a human-readable backend name (e.g. "bubblewrap", "firejail").
	Name() string

	// Available reports whether the sandbox binary is installed and usable.
	Available() bool

	// WrapCommand takes a command and its args and returns a modified command
	// that runs inside the sandbox, or an error if sandboxing is not possible.
	WrapCommand(ctx context.Context, workspaceRoot string, cmd string, args ...string) (*exec.Cmd, error)
}

// NoopBackend returns a noop (passthrough) sandbox backend.
func NoopBackend() Backend { return &noop{} }

// NewByName returns a sandbox backend by name. Returns a noop backend for
// unknown names or when the requested backend is not available.
func NewByName(name string) Backend {
	switch strings.ToLower(name) {
	case "landlock":
		if lb := NewLandlock(); lb != nil && lb.Available() {
			return newLandlockAdapter(lb)
		}
		return &noop{}
	case "bubblewrap":
		bw := &bubblewrap{}
		if bw.Available() {
			return bw
		}
		return &noop{}
	case "firejail":
		fj := &firejail{}
		if fj.Available() {
			return fj
		}
		return &noop{}
	case "docker":
		dk := &dockerBackend{}
		if dk.Available() {
			return dk
		}
		return &noop{}
	case "noop", "none", "disabled":
		return &noop{}
	default:
		return &noop{}
	}
}

// Detect returns the best available sandbox backend for the current platform.
// Priority: Landlock (kernel, no deps) > bubblewrap > firejail > docker > noop.
func Detect() Backend {
	switch runtime.GOOS {
	case "linux":
		if lb := NewLandlock(); lb != nil && lb.Available() {
			return newLandlockAdapter(lb)
		}
		bw := &bubblewrap{}
		if bw.Available() {
			return bw
		}
		fj := &firejail{}
		if fj.Available() {
			return fj
		}
	}
	// Docker works on any platform with Docker installed, but is heavier.
	dk := &dockerBackend{}
	if dk.Available() {
		return dk
	}
	return &noop{}
}

// landlockAdapter wraps LandlockBackend to conform to the Backend interface.
type landlockAdapter struct{ inner *LandlockBackend }

func newLandlockAdapter(lb *LandlockBackend) *landlockAdapter { return &landlockAdapter{inner: lb} }
func (a *landlockAdapter) Name() string                       { return a.inner.Name() }
func (a *landlockAdapter) Available() bool                    { return a.inner.Available() }
func (a *landlockAdapter) WrapCommand(ctx context.Context, workspaceRoot string, cmd string, args ...string) (*exec.Cmd, error) {
	c := exec.CommandContext(ctx, cmd, args...)
	c.Dir = workspaceRoot
	return a.inner.WrapCommand(c, workspaceRoot)
}

// ── Bubblewrap ──────────────────────────────────────────────────────────

type bubblewrap struct{}

func (b *bubblewrap) Name() string { return "bubblewrap" }

func (b *bubblewrap) Available() bool {
	_, err := exec.LookPath("bwrap")
	return err == nil
}

func (b *bubblewrap) WrapCommand(ctx context.Context, workspaceRoot string, cmd string, args ...string) (*exec.Cmd, error) {
	// Build a minimal filesystem namespace:
	// - Mount workspace as read-write under /workspace
	// - Mount /usr, /lib, /lib64, /bin as read-only
	// - Create minimal /tmp, /proc
	// - No network access (--unshare-net)
	bwrapArgs := []string{
		"--unshare-net",
		"--unshare-ipc",
		"--unshare-pid",
		"--unshare-uts",
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
		"--ro-bind", "/usr", "/usr",
		"--ro-bind", "/lib", "/lib",
		"--ro-bind", "/bin", "/bin",
		"--bind", workspaceRoot, workspaceRoot,
		"--chdir", workspaceRoot,
	}

	// Mount /lib64 if it exists (common on x86_64)
	if pathExists("/lib64") {
		bwrapArgs = append(bwrapArgs, "--ro-bind", "/lib64", "/lib64")
	}

	// Mount /etc for DNS resolution
	if pathExists("/etc") {
		bwrapArgs = append(bwrapArgs, "--ro-bind", "/etc", "/etc")
	}

	bwrapArgs = append(bwrapArgs, "--")
	bwrapArgs = append(bwrapArgs, cmd)
	bwrapArgs = append(bwrapArgs, args...)

	return exec.CommandContext(ctx, "bwrap", bwrapArgs...), nil
}

// ── Firejail ────────────────────────────────────────────────────────────

type firejail struct{}

func (f *firejail) Name() string { return "firejail" }

func (f *firejail) Available() bool {
	_, err := exec.LookPath("firejail")
	return err == nil
}

func (f *firejail) WrapCommand(ctx context.Context, workspaceRoot string, cmd string, args ...string) (*exec.Cmd, error) {
	fjArgs := []string{
		"--net=none",
		"--private-tmp",
		"--quiet",
		"--noprofile",
		fmt.Sprintf("--whitelist=%s", workspaceRoot),
		fmt.Sprintf("--private=%s", workspaceRoot),
	}
	fjArgs = append(fjArgs, cmd)
	fjArgs = append(fjArgs, args...)

	return exec.CommandContext(ctx, "firejail", fjArgs...), nil
}

// ── Noop ────────────────────────────────────────────────────────────────

type noop struct{}

func (n *noop) Name() string { return "noop" }

func (n *noop) Available() bool { return true }

func (n *noop) WrapCommand(ctx context.Context, workspaceRoot string, cmd string, args ...string) (*exec.Cmd, error) {
	// On platforms without sandbox support, run the command directly.
	all := append([]string{cmd}, args...)
	c := exec.CommandContext(ctx, all[0], all[1:]...)
	c.Dir = workspaceRoot
	return c, nil
}

// ── Helpers ─────────────────────────────────────────────────────────────

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// ── Docker ────────────────────────────────────────────────────────────────

type dockerBackend struct{}

func (d *dockerBackend) Name() string { return "docker" }

func (d *dockerBackend) Available() bool {
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	// Verify the daemon is reachable — binary presence is not enough.
	// Use a short timeout to avoid blocking startup on a hung daemon.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "info")
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

func (d *dockerBackend) WrapCommand(ctx context.Context, workspaceRoot string, cmd string, args ...string) (*exec.Cmd, error) {
	// Run the command inside a temporary Docker container:
	//   - Network disabled
	//   - Workspace mounted read-write at the same path
	//   - Container removed after execution (--rm)
	//   - Minimal Alpine-based image
	image := os.Getenv("MNEME_SANDBOX_IMAGE")
	if image == "" {
		image = "alpine:latest"
	}

	dockerArgs := []string{
		"run",
		"--rm",
		"--network=none",
		"--read-only",
		"--tmpfs=/tmp:rw,noexec,nosuid,size=64M",
		fmt.Sprintf("--volume=%s:%s:rw", workspaceRoot, workspaceRoot),
		"--workdir", workspaceRoot,
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		image,
		cmd,
	}
	dockerArgs = append(dockerArgs, args...)

	return exec.CommandContext(ctx, "docker", dockerArgs...), nil
}

// ── Helpers ─────────────────────────────────────────────────────────────

// PathExists reports whether a path exists on the filesystem.
func PathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// EscapeShellArg escapes a string for safe use in a shell command.
// This is a convenience for callers that need to compose shell invocations.
func EscapeShellArg(s string) string {
	// Wrap in single quotes, escaping any embedded single quotes.
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
