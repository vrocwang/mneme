package about

// RPC provides Wails-bound version/update methods.
type RPC struct{}

// NewRPC creates an about RPC handler.
func NewRPC() *RPC {
	return &RPC{}
}

// GetCurrentVersion returns the application version.
func (r *RPC) GetCurrentVersion() string {
	return "0.1.0"
}

// CheckForUpdate checks for available updates.
func (r *RPC) CheckForUpdate() map[string]interface{} {
	return map[string]interface{}{
		"current":          "0.1.0",
		"latest":           "0.1.0",
		"update_available": false,
	}
}
