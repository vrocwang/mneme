//go:build linux

package cwd_jail

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

// Landlock syscall constants (Linux 5.13+ stable kernel ABI).
const (
	landlockCreateRuleset = 444
	landlockAddRule       = 445
	landlockRestrictSelf  = 446

	landlockRulePathBeneath = 1

	landlockAccessFSAll = (1 << 13) - 1
	// readOnlyAccess: execute + read + read_dir
	landlockAccessFSReadOnly = (1 << 0) | (1 << 2) | (1 << 3)

	oPath = 0x200000 // linux O_PATH
)

type landlockRulesetAttr struct {
	handledAccessFS uint64
}

type landlockPathBeneathAttr struct {
	allowedAccess uint64
	parentFD      int32
}

// landlockBackend enforces jail restrictions using kernel Landlock LSM.
// Requires Linux 5.13+ with CONFIG_SECURITY_LANDLOCK=y.
type landlockBackend struct{}

func NewLandlock() Backend {
	return &landlockBackend{}
}

func (l *landlockBackend) Name() string      { return "landlock" }
func (l *landlockBackend) IsAvailable() bool { return isLandlockSupported() }

func (l *landlockBackend) Spawn(ctx context.Context, jail *Jail, cmd string, args ...string) (*exec.Cmd, error) {
	if err := applyJailLandlock(jail); err != nil {
		return nil, fmt.Errorf("landlock: %w", err)
	}

	all := append([]string{cmd}, args...)
	c := exec.CommandContext(ctx, all[0], all[1:]...)
	c.Dir = jail.Root
	return c, nil
}

func isLandlockSupported() bool {
	if _, err := os.Stat("/proc/sys/kernel/landlock"); os.IsNotExist(err) {
		return false
	}
	var uts syscall.Utsname
	if err := syscall.Uname(&uts); err != nil {
		return false
	}
	release := int8ToString(uts.Release[:])
	var major, minor int
	fmt.Sscanf(release, "%d.%d", &major, &minor)
	return major > 5 || (major == 5 && minor >= 13)
}

func applyJailLandlock(jail *Jail) error {
	attr := landlockRulesetAttr{handledAccessFS: landlockAccessFSAll}
	rulesetFD, _, errno := syscall.Syscall(landlockCreateRuleset,
		uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if errno != 0 {
		return fmt.Errorf("landlock_create_ruleset: %s", errno.Error())
	}
	defer syscall.Close(int(rulesetFD))

	// Jail root gets full access (or read-only based on jail config).
	rootAccess := uint64(landlockAccessFSAll)
	if jail.ReadOnly {
		rootAccess = landlockAccessFSReadOnly
	}
	addPathRule(int(rulesetFD), jail.Root, rootAccess)

	// System paths needed for basic execution.
	readOnlyPaths := []string{"/usr", "/lib", "/lib64", "/bin", "/etc", "/dev", "/proc", "/sys", "/tmp"}
	for _, p := range readOnlyPaths {
		if _, err := os.Stat(p); err == nil {
			addPathRule(int(rulesetFD), p, landlockAccessFSReadOnly)
		}
	}

	_, _, errno = syscall.Syscall(landlockRestrictSelf, uintptr(rulesetFD), 0, 0)
	if errno != 0 {
		return fmt.Errorf("landlock_restrict_self: %s", errno.Error())
	}
	return nil
}

func addPathRule(rulesetFD int, path string, access uint64) error {
	fd, err := openPathFD(path)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)

	pathAttr := landlockPathBeneathAttr{
		allowedAccess: access,
		parentFD:      int32(fd),
	}
	_, _, errno := syscall.Syscall(landlockAddRule,
		uintptr(rulesetFD), uintptr(landlockRulePathBeneath),
		uintptr(unsafe.Pointer(&pathAttr)))
	if errno != 0 {
		return fmt.Errorf("add_rule %q: %s", path, errno.Error())
	}
	return nil
}

func openPathFD(path string) (int, error) {
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

func int8ToString(arr []int8) string {
	var b []byte
	for _, v := range arr {
		if v == 0 {
			break
		}
		b = append(b, byte(v))
	}
	return string(b)
}
