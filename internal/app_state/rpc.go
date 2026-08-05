package app_state

import (
	"database/sql"

	"github.com/simon/mneme/internal/approval"
	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/inference"
)

// AppStateRPC provides Wails-bound application state methods for the settings UI.
type AppStateRPC struct {
	capReg       *capability.CapabilityRegistry
	approvalGate *approval.Gate
	provider     inference.Provider
	db           *sql.DB
}

// NewAppStateRPC creates an application state RPC handler.
// SetCapReg updates the capability registry reference after startup.
func (r *AppStateRPC) SetCapReg(capReg *capability.CapabilityRegistry) { r.capReg = capReg }

// SetProvider updates the provider reference after startup.
func (r *AppStateRPC) SetProvider(provider inference.Provider) { r.provider = provider }

// SetDB updates the database reference after startup.
func (r *AppStateRPC) SetDB(db *sql.DB) { r.db = db }

// NewAppStateRPC creates an application state RPC handler.
func NewAppStateRPC(
	capReg *capability.CapabilityRegistry,
	approvalGate *approval.Gate,
	provider inference.Provider,
	db *sql.DB,
) *AppStateRPC {
	return &AppStateRPC{
		capReg:       capReg,
		approvalGate: approvalGate,
		provider:     provider,
		db:           db,
	}
}

// AppStateSnapshot returns a cross-cutting application state summary.
func (r *AppStateRPC) AppStateSnapshot() map[string]interface{} {
	toolCount := 0
	agentCount := 0
	if r.capReg != nil {
		toolCount = len(r.capReg.AllTools())
		agentCount = len(r.capReg.AllAgents())
	}

	pendingApprovals := 0
	if r.approvalGate != nil {
		pendingApprovals = len(r.approvalGate.ListPending())
	}

	providerReady := r.provider != nil

	dbReady := false
	if r.db != nil {
		dbReady = r.db.Ping() == nil
	}

	return map[string]interface{}{
		"ok":                true,
		"tool_count":        toolCount,
		"agent_count":       agentCount,
		"pending_approvals": pendingApprovals,
		"provider_ready":    providerReady,
		"db_ready":          dbReady,
	}
}
