package workflows

// WorkflowRPC provides Wails-bound methods for workflow installation from the
// frontend. Registered via capability.RegisterWailsRPC in RegisterAll.
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

// InstallWorkflowFromURL fetches a SKILL.md from a URL and installs it as a workflow.
func (r *WorkflowRPC) InstallWorkflowFromURL(url string) (string, error) {
	return InstallFromURL(r.skillsDir, url, 0)
}

// UninstallWorkflow removes a user-installed workflow by name.
func (r *WorkflowRPC) UninstallWorkflow(name string) error {
	return Uninstall(r.skillsDir, name)
}
