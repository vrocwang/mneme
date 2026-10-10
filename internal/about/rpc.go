package about

import (
	"context"
	"encoding/json"
	"fmt"
)

// Version is the application version reported to the UI. It is defined once so
// the version shown and the version embedded in update checks cannot drift.
const Version = "0.1.0"

// UpdateChecker runs the real update check and returns the extension's JSON
// output. The check itself lives in the tool-update extension (it needs network
// access and a release API); this package only owns the UI-facing RPC.
type UpdateChecker func(ctx context.Context) (string, error)

// RPC provides Wails-bound version/update methods.
type RPC struct {
	check UpdateChecker
}

// NewRPC creates an about RPC handler.
func NewRPC() *RPC {
	return &RPC{}
}

// SetUpdateChecker injects the update-check backend after startup. Without it
// CheckForUpdate reports the running version and marks itself unsupported
// instead of pretending the app is up to date.
func (r *RPC) SetUpdateChecker(f UpdateChecker) { r.check = f }

// GetCurrentVersion returns the application version.
func (r *RPC) GetCurrentVersion() string {
	return Version
}

// CheckForUpdate reports whether a newer release is available. It delegates to
// the tool-update extension when one is installed; otherwise it says so.
func (r *RPC) CheckForUpdate() map[string]interface{} {
	if r.check == nil {
		return map[string]interface{}{
			"current":          Version,
			"latest":           Version,
			"update_available": false,
			"supported":        false,
			"reason":           "update checking is provided by the tool-update extension, which is not installed",
		}
	}
	out, err := r.check(context.Background())
	if err != nil {
		return map[string]interface{}{
			"current":          Version,
			"update_available": false,
			"supported":        true,
			"error":            err.Error(),
		}
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		return map[string]interface{}{
			"current":          Version,
			"update_available": false,
			"supported":        true,
			"error":            fmt.Sprintf("update_check returned unreadable output: %v", err),
		}
	}
	result["supported"] = true
	return result
}
