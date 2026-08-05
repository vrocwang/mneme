//go:build linux
// +build linux

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// Landlock syscall numbers and constants (Linux 5.13+ stable kernel ABI).
const (
	landlockCreateRuleset = 444
	landlockAddRule       = 445
	landlockRestrictSelf  = 446

	landlockRulePathBeneath = 1

	landlockAccessExec       = 1 << 0
	landlockAccessWrite      = 1 << 1
	landlockAccessRead       = 1 << 2
	landlockAccessReadDir    = 1 << 3
	landlockAccessRemoveDir  = 1 << 4
	landlockAccessRemoveFile = 1 << 5
	landlockAccessMakeChar   = 1 << 6
	landlockAccessMakeDir    = 1 << 7
	landlockAccessMakeReg    = 1 << 8
	landlockAccessMakeSock   = 1 << 9
	landlockAccessMakeFifo   = 1 << 10
	landlockAccessMakeBlock  = 1 << 11
	landlockAccessMakeSym    = 1 << 12

	landlockAllAccess = landlockAccessExec | landlockAccessWrite | landlockAccessRead |
		landlockAccessReadDir | landlockAccessRemoveDir | landlockAccessRemoveFile |
		landlockAccessMakeChar | landlockAccessMakeDir | landlockAccessMakeReg |
		landlockAccessMakeSock | landlockAccessMakeFifo | landlockAccessMakeBlock |
		landlockAccessMakeSym

	landlockReadOnlyAccess = landlockAccessExec | landlockAccessRead | landlockAccessReadDir

	oPath = 0x200000 // linux O_PATH
)

// landlockRulesetAttr is the argument struct for landlock_create_ruleset.
type landlockRulesetAttr struct {
	handledAccessFS uint64
}

// landlockPathBeneathAttr is the argument struct for a path_beneath rule.
type landlockPathBeneathAttr struct {
	allowedAccess uint64
	parentFD      int32
}

// LandlockBackend applies kernel Landlock LSM rules to restrict filesystem
// access. Requires Linux 5.13+ with CONFIG_SECURITY_LANDLOCK=y.
//
// Landlock is applied to the current Go process — matching the Rust
// behavior where the agent process is restricted to its workspace and
// required system paths. This is safe because:
//   - The desktop app operates within its data/workspace directories
//   - System paths are allowed as read-only
//   - External MCP servers or extensions are spawned through separate
//     processes before Landlock is applied
type LandlockBackend struct {
	applied bool // tracks whether restrictions have been applied
}

func NewLandlock() *LandlockBackend {
	return &LandlockBackend{}
}

func (l *LandlockBackend) Name() string { return "landlock" }

// Available returns true when the kernel supports Landlock (5.13+).
func (l *LandlockBackend) Available() bool {
	return IsLandlockSupported()
}

// WrapCommand applies Landlock filesystem restrictions to the current
// process, then returns the command unchanged. Landlock is applied only
// once — subsequent calls are a no-op.
//
// The workspace directory gets full read-write access. Standard system
// paths (/usr, /lib, /etc, /proc, /dev, /bin, /tmp) get read-only
// access. All other paths are inaccessible.
func (l *LandlockBackend) WrapCommand(cmd *exec.Cmd, workspace string) (*exec.Cmd, error) {
	if l.applied {
		return cmd, nil
	}

	if err := applyLandlockRules(workspace); err != nil {
		return nil, fmt.Errorf("landlock: %w", err)
	}

	l.applied = true
	return cmd, nil
}

// IsLandlockSupported checks whether Landlock is available at runtime.
func IsLandlockSupported() bool {
	// Check for the Landlock control file (kernel 5.13+).
	if _, err := os.Stat("/proc/sys/kernel/landlock"); os.IsNotExist(err) {
		return false
	}

	var uts syscall.Utsname
	if err := syscall.Uname(&uts); err != nil {
		return false
	}

	release := int8ToString(uts.Release[:])
	parts := strings.Split(release, ".")
	if len(parts) < 2 {
		return false
	}

	var major, minor int
	fmt.Sscanf(parts[0], "%d", &major)
	fmt.Sscanf(parts[1], "%d", &minor)

	return major > 5 || (major == 5 && minor >= 13)
}

// applyLandlockRules restricts the current process's filesystem access
// using Landlock. The workspace dir gets full access; system paths get
// read-only access.
func applyLandlockRules(workspace string) error {
	// Resolve workspace to absolute path for reliable fd operations.
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}

	// Build the ruleset: handle all known access types.
	attr := landlockRulesetAttr{handledAccessFS: landlockAllAccess}
	rulesetFD, _, errno := syscall.Syscall(landlockCreateRuleset, uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if errno != 0 {
		return fmt.Errorf("landlock_create_ruleset: %s", errno.Error())
	}
	defer syscall.Close(int(rulesetFD))

	// Allow full read-write access to the workspace directory.
	if err := addPathRule(int(rulesetFD), absWorkspace, landlockAllAccess); err != nil {
		return fmt.Errorf("workspace rule: %w", err)
	}

	// Allow read-only access to standard system paths. These are needed
	// for basic binary execution, library loading, and system interaction.
	readOnlyPaths := []string{
		"/usr", "/lib", "/lib64", "/bin", "/etc",
		"/dev", "/proc", "/sys", "/tmp",
	}
	for _, p := range readOnlyPaths {
		if _, err := os.Stat(p); err == nil {
			if err := addPathRule(int(rulesetFD), p, landlockReadOnlyAccess); err != nil {
				// Non-fatal: some paths may not exist in all environments
				// (e.g. /lib64 on pure 64-bit systems without compat).
				continue
			}
		}
	}

	// Apply the ruleset. After this call, the restrictions are
	// permanent for this process and all future children.
	_, _, errno = syscall.Syscall(landlockRestrictSelf, uintptr(rulesetFD), 0, 0)
	if errno != 0 {
		return fmt.Errorf("landlock_restrict_self: %s", errno.Error())
	}

	return nil
}

// addPathRule adds a path_beneath rule to a Landlock ruleset.
func addPathRule(rulesetFD int, path string, access uint64) error {
	parentFD, err := openPath(path)
	if err != nil {
		return fmt.Errorf("open %q: %w", path, err)
	}
	defer syscall.Close(parentFD)

	pathAttr := landlockPathBeneathAttr{
		allowedAccess: access,
		parentFD:      int32(parentFD),
	}

	_, _, errno := syscall.Syscall(landlockAddRule,
		uintptr(rulesetFD),
		uintptr(landlockRulePathBeneath),
		uintptr(unsafe.Pointer(&pathAttr)),
	)
	if errno != 0 {
		return fmt.Errorf("add_rule path_beneath %q: %s", path, errno.Error())
	}

	return nil
}

// openPath opens a directory for use as a Landlock parent_fd.
func openPath(path string) (int, error) {
	info, err := os.Stat(path)
	if err != nil {
		return -1, err
	}
	target := path
	if !info.IsDir() {
		target = filepath.Dir(path)
	}
	return syscall.Open(target, syscall.O_RDONLY|syscall.O_CLOEXEC|oPath, 0)
}

// int8ToString converts a null-terminated int8 array to a string.
func int8ToString(arr []int8) string {
	var b strings.Builder
	for _, v := range arr {
		if v == 0 {
			break
		}
		b.WriteByte(byte(v))
	}
	return b.String()
}
