package workflows

import "github.com/simon/mneme/internal/capability"

// WorkflowRPC provides Wails-bound methods for workflow installation from the
// frontend.
type WorkflowRPC struct {
	skillsDir string
}

// NewWorkflowRPC creates a Wails RPC handler for workflow install/uninstall.
func NewWorkflowRPC(skillsDir string) *WorkflowRPC {
	return &WorkflowRPC{skillsDir: skillsDir}
}

// RegisterRPC implements capability.WailsRPCRegistrar.
func (r *WorkflowRPC) RegisterRPC() []interface{} {
	return []interface{}{r}
}

// RegisterRPC adds the workflow install/uninstall RPC to the Wails Bind list.
// It must be called before wails.Run builds that list, so it is wired from the
// boot layer rather than from RegisterAll (which runs after OnStartup).
func RegisterRPC(skillsDir string) {
	capability.RegisterWailsRPC(NewWorkflowRPC(skillsDir))
}

// InstallWorkflowFromURL fetches a SKILL.md from a URL and installs it as a workflow.
func (r *WorkflowRPC) InstallWorkflowFromURL(url string) (string, error) {
	return InstallFromURL(r.skillsDir, url, 0)
}

// UninstallWorkflow removes a user-installed workflow by name.
func (r *WorkflowRPC) UninstallWorkflow(name string) error {
	return Uninstall(r.skillsDir, name)
}
