package security

import (
	"context"
	"encoding/json"
	"fmt"
)

// pathParamNames lists argument keys that may contain filesystem paths.
// Tools using parameter names outside this list (e.g. "input", "output", "root")
// will NOT have their paths validated — tool authors must use one of these
// canonical names for path arguments.
var pathParamNames = []string{
	"path", "file_path", "target", "source", "destination", "dir",
	"input", "output", "root", "file", "from", "to", "cwd",
}

// BuildPolicyChecker returns a function suitable for ToolExecConfig.PolicyChecker.
// It enforces tier-based access, path validation, and routes external-effect calls
// through the approval gate when required.
func BuildPolicyChecker(tier Tier, policy *SecurityPolicy, approve ApproveFunc, hasExternalEffect func(toolName string, args map[string]interface{}) bool) func(ctx context.Context, toolName string, args map[string]interface{}) error {
	return func(ctx context.Context, toolName string, args map[string]interface{}) error {
		// Validate file paths in args against the security policy.
		if policy != nil {
			for _, pathKey := range pathParamNames {
				path, ok := args[pathKey].(string)
				if !ok {
					continue
				}
				if path != "" && !policy.IsPathAllowed(path) {
					return fmt.Errorf("path %q is not allowed by security policy", path)
				}
			}
		}

		// In read-only tier, block only external-effect (write) operations.
		// Read-only tools (file_read, grep, memory_search, etc.) are allowed.
		if tier == TierReadOnly && hasExternalEffect != nil {
			if hasExternalEffect(toolName, args) {
				return fmt.Errorf("write operation %q blocked in read-only tier", toolName)
			}
			return nil
		}

		// In supervised tier, ONLY external-effect tools require approval.
		// Read-only tools (like file_read, grep, memory_search) are allowed
		// without prompting — matching the Rust gate_decision contract.
		if tier == TierSupervised && approve != nil && hasExternalEffect != nil {
			if !hasExternalEffect(toolName, args) {
				return nil
			}
			reason := fmt.Sprintf("tool %q requires approval in supervised mode", toolName)
			argsJSON, err := json.Marshal(args)
			if err != nil {
				argsJSON = []byte("{}")
			}
			decision, err := approve(ctx, toolName, string(argsJSON), reason)
			if err != nil {
				return fmt.Errorf("approval error: %w", err)
			}
			if decision == "deny" {
				return fmt.Errorf("tool %q denied by user", toolName)
			}
		}

		return nil
	}
}

// ApproveFunc is called by the PolicyChecker to request user approval for a tool call.
// It returns the decision ("approve_once", "approve_always", "deny") or an error.
type ApproveFunc func(ctx context.Context, toolName, argsJSON, reason string) (string, error)
