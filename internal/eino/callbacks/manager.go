package callbacks

import (
	"github.com/simon/mneme/internal/agent"
	"github.com/simon/mneme/internal/learning"
	"github.com/simon/mneme/internal/security"
)

// Manager holds all callback wrappers used by the eino agent pipeline.
// Each callback is nil-safe: methods check their wrapped object before calling.
//
// Streaming is handled separately via the onEvent parameter passed directly
// to Runner.StreamQuery — there is no StreamingCallback in the Manager
// because the Runner never reads from it (the caller provides its own sink).
type Manager struct {
	Audit    *AuditCallback
	Cost     *CostCallback
	Learning *LearningCallback
}

// NewManager creates a Manager from the concrete backend types.
// Any nil argument is accepted; the corresponding callback simply no-ops.
func NewManager(
	auditLogger *security.AuditLogger,
	costTracker *agent.DailyCostTracker,
	learningEngine *learning.Engine,
) *Manager {
	return &Manager{
		Audit:    NewAuditCallback(auditLogger),
		Cost:     NewCostCallback(costTracker),
		Learning: NewLearningCallback(learningEngine),
	}
}
