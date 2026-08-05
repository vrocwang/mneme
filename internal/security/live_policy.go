package security

import (
	"sync/atomic"
)

// livePolicy holds the process-global SecurityPolicy, swappable at runtime
// via ReloadLivePolicy. It is read on every command/path gating check.
var livePolicy atomic.Value

// liveActionDir holds the current action_dir separate from the policy so
// it can be updated independently (when agent-paths config changes) without
// rebuilding the full policy. ReloadLivePolicy picks this up.
var liveActionDir atomic.Value // stores string

func init() {
	// Default: supervised, no workspace restriction.
	livePolicy.Store(&SecurityPolicy{
		WorkspaceOnly: false,
		WorkspaceRoot: "",
		ActionDir:     "",
	})
	liveActionDir.Store("")
}

// LivePolicy returns the current process-global SecurityPolicy.
// This is the canonical policy for all runtime gate decisions.
func LivePolicy() *SecurityPolicy {
	v := livePolicy.Load()
	if v == nil {
		return &SecurityPolicy{WorkspaceOnly: false}
	}
	return v.(*SecurityPolicy)
}

// ReloadLivePolicy atomically swaps the process-global policy.
// Call this when the user updates their autonomy settings at runtime.
// If a pending action_dir was set via SetActionDir (and not yet reflected
// in the policy), it is applied to the new policy before swapping.
func ReloadLivePolicy(p *SecurityPolicy) {
	if p == nil {
		return
	}
	// Apply any pending action_dir that was set independently.
	if dir := liveActionDir.Load(); dir != nil {
		if s, ok := dir.(string); ok && s != "" {
			p.ActionDir = s
		}
	}
	livePolicy.Store(p)
}

// SetActionDir stores a new action_dir and atomically swaps it on the
// current live policy. Use this when the agent-paths config changes at
// runtime — it avoids rebuilding the full policy just for a path change.
// The new value is also stored so a subsequent ReloadLivePolicy will keep it.
func SetActionDir(newDir string) {
	liveActionDir.Store(newDir)

	v := livePolicy.Load()
	if v == nil {
		return
	}
	current := v.(*SecurityPolicy)
	if current == nil {
		return
	}

	// Clone the current policy with the new action_dir.
	clone := *current
	clone.ActionDir = newDir
	livePolicy.Store(&clone)
}

// ActionDir returns the current action_dir from the live policy.
func ActionDir() string {
	p := LivePolicy()
	if p == nil {
		return ""
	}
	return p.ActionDir
}
