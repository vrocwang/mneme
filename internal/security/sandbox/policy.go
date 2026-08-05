package sandbox

import (
	"runtime"
)

// Policy determines which sandbox backend to use for tool execution.
type Policy string

const (
	PolicyAuto     Policy = "auto"       // pick best available
	PolicyNone     Policy = "none"       // no sandbox
	PolicyLandlock Policy = "landlock"   // force Landlock (Linux only)
	PolicyBwrap    Policy = "bubblewrap" // force bubblewrap
	PolicyFirejail Policy = "firejail"   // force firejail
	PolicyDocker   Policy = "docker"     // force docker
)

// PolicyConfig holds sandbox policy configuration.
type PolicyConfig struct {
	// DefaultPolicy is the fallback policy when no per-tool override exists.
	DefaultPolicy Policy `json:"default_policy"`

	// PerToolPolicy maps tool names to specific sandbox policies.
	PerToolPolicy map[string]Policy `json:"per_tool,omitempty"`

	// DockerImage overrides the default Alpine image.
	DockerImage string `json:"docker_image,omitempty"`

	// ReadOnlyMounts are extra paths to mount read-only inside the sandbox.
	ReadOnlyMounts []string `json:"ro_mounts,omitempty"`
}

// DefaultPolicyConfig returns a safe default configuration.
func DefaultPolicyConfig() PolicyConfig {
	return PolicyConfig{
		DefaultPolicy: PolicyAuto,
		DockerImage:   "alpine:latest",
	}
}

// ResolvePolicy determines the effective sandbox policy for a given tool.
func ResolvePolicy(cfg PolicyConfig, toolName string) Policy {
	if p, ok := cfg.PerToolPolicy[toolName]; ok {
		return p
	}
	return cfg.DefaultPolicy
}

// ResolveBackend returns the Backend for the given policy.
func ResolveBackend(policy Policy) Backend {
	switch policy {
	case PolicyNone:
		return &noop{}
	case PolicyLandlock:
		if runtime.GOOS == "linux" {
			lb := NewLandlock()
			if lb != nil && lb.Available() {
				return newLandlockAdapter(lb)
			}
		}
		return &noop{}
	case PolicyBwrap:
		bw := &bubblewrap{}
		if bw.Available() {
			return bw
		}
		return &noop{}
	case PolicyFirejail:
		fj := &firejail{}
		if fj.Available() {
			return fj
		}
		return &noop{}
	case PolicyDocker:
		dk := &dockerBackend{}
		if dk.Available() {
			return dk
		}
		return &noop{}
	default:
		return Detect()
	}
}

// IsElevatedOp returns true for operations that need to escape the sandbox
// (e.g., system package installation, Docker-in-Docker).
func IsElevatedOp(cmd string) bool {
	elevated := map[string]bool{
		"apt": true, "apt-get": true, "dnf": true, "yum": true,
		"pacman": true, "brew": true, "zypper": true,
		"docker": true, "podman": true,
		"systemctl": true, "service": true,
		"mount": true, "umount": true,
		"modprobe": true, "insmod": true, "rmmod": true,
	}
	return elevated[cmd]
}

// BuildElevatedOp wraps a command for elevated execution outside the sandbox,
// with explicit user confirmation required.
func BuildElevatedOp(cmd string, args []string, policy Policy) (string, []string, bool) {
	if policy == PolicyNone || policy == PolicyDocker {
		return cmd, args, false // already elevated or no sandbox
	}
	// For sandboxed backends, elevated ops need explicit approval.
	// The caller must confirm via the approval gate before executing.
	return cmd, args, true
}
