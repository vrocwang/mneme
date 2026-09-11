package about

// Version is the application version reported to the UI. It is defined once so
// the version shown and the version embedded in update checks cannot drift.
const Version = "0.1.0"

// RPC provides Wails-bound version/update methods.
type RPC struct{}

// NewRPC creates an about RPC handler.
func NewRPC() *RPC {
	return &RPC{}
}

// GetCurrentVersion returns the application version.
func (r *RPC) GetCurrentVersion() string {
	return Version
}

// CheckForUpdate reports whether a newer release is available.
//
// The real check is performed by the optional tool-update extension
// (update_check / update_apply); this in-process method only reports the
// running version and never claims an update exists. Wiring it to the
// extension is tracked as a follow-up, so the response explicitly marks
// itself unsupported rather than implying an up-to-date result.
func (r *RPC) CheckForUpdate() map[string]interface{} {
	return map[string]interface{}{
		"current":          Version,
		"latest":           Version,
		"update_available": false,
		"supported":        false,
	}
}
