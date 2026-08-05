package connectivity

import (
	"github.com/simon/mneme/internal/capability"
)

// ConnectivityRPC provides Wails-bound methods for network diagnostics.
type ConnectivityRPC struct{}

// RegisterRPC implements capability.WailsRPCRegistrar.
func (r *ConnectivityRPC) RegisterRPC() []interface{} { return []interface{}{r} }

// RunDiagnosticsRPC runs a full connectivity check and returns the status.
func (r *ConnectivityRPC) RunDiagnosticsRPC() *Status {
	return RunDiagnostics(nil)
}

// QuickCheckRPC returns true if basic internet connectivity is available.
func (r *ConnectivityRPC) QuickCheckRPC() bool {
	return QuickCheck()
}

// Register registers connectivity RPC bindings.
func Register() {
	capability.RegisterWailsRPC(&ConnectivityRPC{})
}
