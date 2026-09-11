package capability

import "github.com/simon/mneme/internal/tools"

// RegisterSetTools registers tools into a builtin capability set, creating the
// set on first use. It is the single generic entry point for domain modules
// contributing their tool groups, so callers never repeat the
// create-set-then-register-N-tools boilerplate.
func RegisterSetTools(reg *CapabilityRegistry, setID, setName string, ts ...tools.Tool) {
	if reg == nil {
		return
	}
	reg.mu.Lock()
	reg.ensureSetLocked(setID, setName, KindBuiltin)
	reg.mu.Unlock()
	for _, t := range ts {
		reg.RegisterTool(setID, t)
	}
}
