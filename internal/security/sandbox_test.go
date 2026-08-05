package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsPathAllowed_WorkspaceAllowed(t *testing.T) {
	workspace := t.TempDir()
	policy := &SecurityPolicy{
		WorkspaceOnly: true,
		WorkspaceRoot: workspace,
	}

	allowed := policy.IsPathAllowed(filepath.Join(workspace, "projects", "file.txt"))
	if !allowed {
		t.Error("workspace path should be allowed")
	}
}

func TestIsPathAllowed_OutsideWorkspaceBlocked(t *testing.T) {
	workspace := t.TempDir()
	policy := &SecurityPolicy{
		WorkspaceOnly: true,
		WorkspaceRoot: workspace,
	}

	allowed := policy.IsPathAllowed("/etc/passwd")
	if allowed {
		t.Error("/etc/passwd should be blocked")
	}
}

func TestIsPathAllowed_TrustedRoot(t *testing.T) {
	workspace := t.TempDir()
	trusted := t.TempDir()
	policy := &SecurityPolicy{
		WorkspaceOnly: true,
		WorkspaceRoot: workspace,
		TrustedRoots:  []TrustedRoot{{Path: trusted, Access: "readwrite"}},
	}

	allowed := policy.IsPathAllowed(filepath.Join(trusted, "file.txt"))
	if !allowed {
		t.Error("trusted root path should be allowed")
	}
}

func TestAlwaysForbiddenPaths(t *testing.T) {
	policy := &SecurityPolicy{WorkspaceOnly: false}
	paths := alwaysForbiddenList()
	for _, path := range paths {
		if policy.IsPathAllowed(path) {
			t.Errorf("%s should always be forbidden", path)
		}
	}
}

func TestValidatePath_OutsideWorkspaceBlocked(t *testing.T) {
	workspace := t.TempDir()
	_, err := ValidatePath("/etc/hosts", workspace)
	if err == nil {
		t.Error("ValidatePath should block path outside workspace root")
	}
}

func TestValidatePath_InsideWorkspaceAllowed(t *testing.T) {
	workspace := t.TempDir()
	allowed := filepath.Join(workspace, "test.txt")
	_, err := ValidatePath(allowed, workspace)
	if err != nil {
		t.Errorf("ValidatePath should allow path inside workspace: %v", err)
	}
}

func TestValidatePath_SymlinkEscapeBlocked(t *testing.T) {
	workspace := t.TempDir()
	symlinkPath := filepath.Join(workspace, "escape")
	// Create a symlink pointing outside the workspace
	target := "/etc/hosts"
	if err := os.Symlink(target, symlinkPath); err != nil {
		t.Skipf("cannot create symlink (may need privileges): %v", err)
	}
	_, err := ValidatePath(symlinkPath, workspace)
	if err == nil {
		t.Error("ValidatePath should block symlink pointing outside workspace")
	}
}

func TestIsPathAllowed_WorkspaceInternalDirBlocked(t *testing.T) {
	workspace := t.TempDir()
	// Create an internal dir under workspace
	memoryDir := filepath.Join(workspace, "memory")
	os.MkdirAll(memoryDir, 0755)

	policy := &SecurityPolicy{
		WorkspaceOnly: true,
		WorkspaceRoot: workspace,
		ActionDir:     filepath.Join(workspace, "projects"),
	}

	allowed := policy.IsPathAllowed(memoryDir)
	if allowed {
		t.Error("workspace-internal 'memory' dir should be blocked even within workspace")
	}
}

func TestIsPathAllowed_WorkspaceInternalFileBlocked(t *testing.T) {
	workspace := t.TempDir()
	coreToken := filepath.Join(workspace, "core.token")
	os.WriteFile(coreToken, []byte("test"), 0600)

	policy := &SecurityPolicy{
		WorkspaceOnly: true,
		WorkspaceRoot: workspace,
	}

	allowed := policy.IsPathAllowed(coreToken)
	if allowed {
		t.Error("workspace-internal 'core.token' file should be blocked")
	}
}

func TestIsPathAllowed_WorkspaceInternalDirAllowedViaTrustedRoot(t *testing.T) {
	workspace := t.TempDir()
	memoryDir := filepath.Join(workspace, "memory")
	os.MkdirAll(memoryDir, 0755)

	// When an explicit trusted root covers the path, the internal-path
	// block is overridden.
	policy := &SecurityPolicy{
		WorkspaceOnly: true,
		WorkspaceRoot: workspace,
		TrustedRoots:  []TrustedRoot{{Path: memoryDir, Access: "readwrite"}},
	}

	allowed := policy.IsPathAllowed(memoryDir)
	if !allowed {
		t.Error("internal dir covered by trusted root should be allowed")
	}
}

func TestIsPathAllowed_ActionDirSeparation(t *testing.T) {
	workspace := t.TempDir()
	actionDir := filepath.Join(workspace, "projects")

	policy := &SecurityPolicy{
		WorkspaceOnly: true,
		WorkspaceRoot: workspace,
		ActionDir:     actionDir,
	}

	// Paths under action_dir should be allowed (it's the agent's working area).
	allowed := policy.IsPathAllowed(filepath.Join(actionDir, "file.txt"))
	if !allowed {
		t.Error("path under action_dir should be allowed")
	}
}

func TestIsPathAllowed_NormalDirUnderWorkspaceAllowed(t *testing.T) {
	workspace := t.TempDir()
	projectsDir := filepath.Join(workspace, "projects")
	os.MkdirAll(projectsDir, 0755)

	policy := &SecurityPolicy{
		WorkspaceOnly: true,
		WorkspaceRoot: workspace,
		ActionDir:     projectsDir,
	}

	// A non-internal directory under workspace should be allowed.
	allowed := policy.IsPathAllowed(filepath.Join(projectsDir, "code", "main.go"))
	if !allowed {
		t.Error("normal directory under workspace should be allowed")
	}
}

func TestValidatePath_WorkspaceInternalDirBlocked(t *testing.T) {
	workspace := t.TempDir()
	memoryDir := filepath.Join(workspace, "memory")
	os.MkdirAll(memoryDir, 0755)

	_, err := ValidatePath(memoryDir, workspace)
	if err == nil {
		t.Error("ValidatePath should block workspace-internal 'memory' dir")
	}
}

func TestSetActionDir(t *testing.T) {
	workspace := t.TempDir()
	actionA := filepath.Join(workspace, "projects-a")
	actionB := filepath.Join(workspace, "projects-b")

	initial := &SecurityPolicy{
		WorkspaceOnly: true,
		WorkspaceRoot: workspace,
		ActionDir:     actionA,
	}
	ReloadLivePolicy(initial)

	if got := ActionDir(); got != actionA {
		t.Errorf("ActionDir() = %q, want %q", got, actionA)
	}

	SetActionDir(actionB)

	if got := ActionDir(); got != actionB {
		t.Errorf("ActionDir() after SetActionDir = %q, want %q", got, actionB)
	}

	// Workspace-only and other fields must survive the swap.
	p := LivePolicy()
	if !p.WorkspaceOnly || p.WorkspaceRoot != workspace {
		t.Error("non-action_dir fields must survive SetActionDir")
	}
}
