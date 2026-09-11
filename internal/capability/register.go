package capability

import "github.com/simon/mneme/internal/tools"

// RegisterSetTools creates (or reuses) a builtin capability set and registers
// the given tools into it in one call. It is the single generic entry point
// for domain modules contributing their tool groups, replacing the repeated
// EnsureSet + N×RegisterTool boilerplate.
func RegisterSetTools(reg *CapabilityRegistry, setID, setName string, ts ...tools.Tool) {
	if reg == nil {
		return
	}
	reg.EnsureSet(&CapabilitySet{
		ID:      setID,
		Name:    setName,
		Kind:    KindBuiltin,
		Enabled: true,
	})
	for _, t := range ts {
		if t != nil {
			reg.RegisterTool(setID, t)
		}
	}
}
