package security

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// WorkspaceInternalDirs lists subdirectories under workspace_dir that hold
// internal application state (memory DBs, sessions, tokens, etc.) and must
// never be writable (or readable) by agent tools.
var WorkspaceInternalDirs = []string{
	"memory",
	"memory_tree",
	"state",
	"approval",
	"sessions",
	"session_raw",
	"cron",
	"devices",
	"mcp_clients",
	"subconscious",
	"vault",
	"task_sources",
	"whatsapp_data",
	"redirect_links",
	"codegraph",
	".mneme",
}

// WorkspaceInternalFiles lists files directly under workspace_dir that hold
// secrets or persona config and must never be writable by agent tools.
var WorkspaceInternalFiles = []string{
	"core.token",
	"dev-keychain.json",
	".env",
	"SOUL.md",
	"IDENTITY.md",
	"HEARTBEAT.md",
	"PROFILE.md",
}

type TrustedRoot struct {
	Path   string
	Access string // "read" or "readwrite"
}

type SecurityPolicy struct {
	WorkspaceOnly bool
	WorkspaceRoot string
	// ActionDir is the agent tool sandbox root. Tools resolve relative paths
	// and default their cwd here instead of WorkspaceRoot. Kept separate so
	// internal state (memory DBs, sessions, tokens) under WorkspaceRoot is
	// not reachable from agent tool calls.
	ActionDir      string
	TrustedRoots   []TrustedRoot
	ForbiddenPaths []string
}

func (p *SecurityPolicy) IsPathAllowed(path string) bool {
	// Block URL-encoded path traversal before any filesystem operations.
	if strings.Contains(path, "..%2f") || strings.Contains(path, "..%2F") ||
		strings.Contains(path, "%2f..") || strings.Contains(path, "%2F..") {
		return false
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	clean := filepath.Clean(abs)

	// Explicitly reject any remaining .. components.
	for _, comp := range strings.Split(clean, string(filepath.Separator)) {
		if comp == ".." {
			return false
		}
	}

	// Resolve symlinks so prefix matching isn't trivially bypassed. resolvePath
	// also canonicalises non-existent leaves via existing ancestors, keeping
	// path resolution consistent with root resolution (resolveRoot).
	clean = resolvePath(clean)

	// Determine whether this path falls under a trusted root (checked before
	// workspace-internal blocking so an explicit trusted-root grant overrides
	// internal-path protection).
	inTrustedRoot := false
	for _, tr := range p.TrustedRoots {
		if pathIsUnder(clean, resolveRoot(tr.Path)) {
			inTrustedRoot = true
			break
		}
	}

	// Block agent access to workspace-internal state paths UNLESS the path
	// falls under an explicitly granted trusted root.
	if !inTrustedRoot {
		if p.isWorkspaceInternalPath(clean) {
			return false
		}
	}

	// Check always-forbidden system paths on the resolved path.
	for _, fb := range alwaysForbiddenList() {
		if pathIsUnder(clean, fb) {
			return false
		}
	}
	for _, fp := range p.ForbiddenPaths {
		if pathIsUnder(clean, resolveRoot(fp)) {
			return false
		}
	}

	if inTrustedRoot {
		// Check credential-store paths even for trusted roots —
		// SSH keys, cloud credentials, and keychains must never be reachable.
		for _, fb := range alwaysForbiddenCredPaths() {
			if pathIsUnder(clean, fb) {
				return false
			}
		}
		return true
	}

	if p.WorkspaceOnly {
		if !pathIsUnder(clean, resolveRoot(p.WorkspaceRoot)) {
			return false
		}
		// Credential paths are always forbidden, even within the workspace.
		for _, fb := range alwaysForbiddenCredPaths() {
			if pathIsUnder(clean, fb) {
				return false
			}
		}
		return true
	}

	// Check credential-store paths (always forbidden regardless of tier).
	for _, fb := range alwaysForbiddenCredPaths() {
		if pathIsUnder(clean, fb) {
			return false
		}
	}

	return true
}

// isWorkspaceInternalPath returns true when the given absolute path points into
// a workspace-internal directory or is a workspace-internal file. These paths
// hold application state (memory DBs, sessions, tokens, persona config) and
// must never be writable by agent tools.
func (p *SecurityPolicy) isWorkspaceInternalPath(absPath string) bool {
	wsRoot := resolveRoot(p.WorkspaceRoot)
	if wsRoot == "" || wsRoot == "." {
		return false
	}

	// Must be under the workspace root.
	if !pathIsUnder(absPath, wsRoot) {
		return false
	}

	rel, err := filepath.Rel(wsRoot, absPath)
	if err != nil {
		return false
	}

	// Check if the first component matches an internal directory.
	parts := strings.SplitN(rel, string(filepath.Separator), 2)
	firstComp := parts[0]
	for _, d := range WorkspaceInternalDirs {
		if firstComp == d {
			return true
		}
	}

	// Check if the relative path exactly matches an internal file (direct child
	// of workspace root).
	if len(parts) == 1 {
		for _, f := range WorkspaceInternalFiles {
			if rel == f {
				return true
			}
		}
	}

	return false
}

// resolveRoot canonicalises a root path by resolving symlinks (e.g. /var ->
// /private/var on macOS) so it matches paths resolved with EvalSymlinks.
// Falls back to a lexical clean when the path does not exist on disk.
func resolveRoot(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.Clean(r)
	}
	return filepath.Clean(p)
}

// resolvePath resolves symlinks on the deepest existing ancestor and appends
// the non-existent remainder, so a path whose leaf does not yet exist (e.g.
// /var/ws/new/file.txt where new/file.txt do not exist) is canonicalised to
// /private/var/ws/new/file.txt on macOS. This keeps leaf canonicalisation
// consistent with resolveRoot regardless of whether the leaf exists.
func resolvePath(p string) string {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.Clean(r)
	}
	cur := p
	parent := filepath.Dir(cur)
	for parent != cur {
		if rp, err := filepath.EvalSymlinks(parent); err == nil {
			rel, _ := filepath.Rel(parent, p)
			return filepath.Clean(filepath.Join(rp, rel))
		}
		cur = parent
		parent = filepath.Dir(cur)
	}
	return p
}

// pathIsUnder returns true when child is equal to parent or is a subdirectory of parent.
// On Windows, comparison is case-insensitive via strings.EqualFold for the prefix
// (filepath.Clean does not normalize case).
func pathIsUnder(child, parent string) bool {
	if child == parent {
		return true
	}
	prefix := parent + string(filepath.Separator)
	if len(child) < len(prefix) {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(child[:len(prefix)], prefix)
	}
	return strings.HasPrefix(child, prefix)
}

func alwaysForbiddenList() []string {
	paths := []string{"/sys/kernel", "/dev/mem"}
	switch runtime.GOOS {
	case "darwin":
		paths = append(paths, "/System/Library", "/etc/master.passwd", "/root",
			"/private/etc/master.passwd", "/private/etc/security")
	case "linux":
		paths = append(paths, "/etc/shadow", "/etc/sudoers", "/etc/sudoers.d",
			"/proc/sys", "/boot", "/root", "/proc", "/sys")
	case "windows":
		paths = append(paths, `C:\Windows\System32\config`, `C:\Windows`, `C:\Windows\System32`)
	}
	return paths
}

// alwaysForbiddenCredPaths returns credential-store paths that are always
// blocked, even from trusted roots. These house SSH keys, cloud credentials,
// and cryptographic keychains that must never be exposed to agent tools.
func alwaysForbiddenCredPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	if home == "" {
		home = "/root"
	}
	dot := func(name string) string { return filepath.Join(home, name) }
	switch runtime.GOOS {
	case "darwin":
		return []string{
			dot(".ssh"), dot(".gnupg"), dot(".aws"), dot(".azure"),
			dot(".kube"), dot(".config/gcloud"),
			home + "/Library/Keychains",
			home + "/Library/Application Support/Google/Chrome",
			home + "/Library/Application Support/Firefox",
		}
	case "linux":
		return []string{
			dot(".ssh"), dot(".gnupg"), dot(".aws"), dot(".azure"),
			dot(".kube"), dot(".config/gcloud"),
			dot(".local/share/keyrings"),
			dot(".password-store"),
		}
	case "windows":
		return []string{
			dot(".ssh"), dot(".gnupg"), dot(".aws"), dot(".azure"),
			dot(".kube"),
			home + `\AppData\Roaming\Microsoft\Credentials`,
			home + `\AppData\Roaming\Microsoft\Protect`,
			home + `\AppData\Local\Microsoft\CredentialManager`,
		}
	default:
		return []string{
			dot(".ssh"), dot(".gnupg"), dot(".aws"), dot(".azure"), dot(".kube"),
		}
	}
}

func ValidatePath(path string, workspaceRoot string) (string, error) {
	if strings.ContainsRune(path, 0) {
		return "", fmt.Errorf("path contains null byte")
	}

	// Block URL-encoded path traversal before any filesystem operations.
	if strings.Contains(path, "..%2f") || strings.Contains(path, "..%2F") ||
		strings.Contains(path, "%2f..") || strings.Contains(path, "%2F..") {
		return "", fmt.Errorf("path contains URL-encoded traversal")
	}

	// Resolve to absolute path first.
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	// Resolve symlinks BEFORE lexical cleanup. This ordering ensures that
	// .. components are resolved by the kernel with symlink context rather
	// than being stripped lexically. For example, if /ws/link → /home,
	// then /ws/link/../.ssh is resolved by the kernel as /home/.ssh, not
	// stripped to /ws/.ssh via lexical .. removal.
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// If the file doesn't exist yet (e.g., write operations), resolve
		// symlinks on the deepest existing ancestor and append the remainder.
		parent := filepath.Dir(abs)
		for parent != abs {
			if rp, perr := filepath.EvalSymlinks(parent); perr == nil {
				rel, _ := filepath.Rel(parent, abs)
				resolved = filepath.Join(rp, rel)
				break
			}
			parent = filepath.Dir(parent)
		}
		if resolved == "" {
			resolved = abs
		}
	}
	clean := filepath.Clean(resolved)

	// Explicitly reject any remaining .. components.
	for _, comp := range strings.Split(clean, string(filepath.Separator)) {
		if comp == ".." {
			return "", fmt.Errorf("path contains unresolved parent directory traversal")
		}
	}

	cleanWS := resolveRoot(workspaceRoot)

	// Verify the resolved path is within the workspace.
	if !pathIsUnder(resolved, cleanWS) {
		return "", fmt.Errorf("path outside workspace: %s", path)
	}

	// Block agent access to workspace-internal state paths.
	if isWorkspaceInternalPathStatic(resolved, cleanWS) {
		return "", fmt.Errorf("path is workspace-internal state and cannot be accessed by agent tools")
	}

	// Check always-forbidden system paths on the resolved path.
	for _, fb := range alwaysForbiddenList() {
		if pathIsUnder(resolved, filepath.Clean(fb)) {
			return "", fmt.Errorf("path is forbidden: %s", fb)
		}
	}
	// Check credential-store paths (always forbidden, even from trusted roots).
	for _, fb := range alwaysForbiddenCredPaths() {
		if pathIsUnder(resolved, fb) {
			return "", fmt.Errorf("path is a protected credential store: %s", fb)
		}
	}

	return resolved, nil
}

// isWorkspaceInternalPathStatic is the static version of isWorkspaceInternalPath
// used by ValidatePath (which doesn't have a SecurityPolicy receiver).
func isWorkspaceInternalPathStatic(absPath, wsRoot string) bool {
	if wsRoot == "" || wsRoot == "." {
		return false
	}
	if !pathIsUnder(absPath, wsRoot) {
		return false
	}
	rel, err := filepath.Rel(wsRoot, absPath)
	if err != nil {
		return false
	}
	parts := strings.SplitN(rel, string(filepath.Separator), 2)
	for _, d := range WorkspaceInternalDirs {
		if parts[0] == d {
			return true
		}
	}
	if len(parts) == 1 {
		for _, f := range WorkspaceInternalFiles {
			if rel == f {
				return true
			}
		}
	}
	return false
}
