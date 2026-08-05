package health

import "database/sql"

// HealthRPC provides Wails-bound health-check methods for the settings UI.
type HealthRPC struct {
	workspace string
	db        *sql.DB
}

// NewHealthRPC creates a health RPC handler.
func NewHealthRPC(workspace string, db *sql.DB) *HealthRPC {
	return &HealthRPC{workspace: workspace, db: db}
}

// DoctorReport runs all registered health checks and returns a report
// matching the frontend DoctorReport interface.
func (h *HealthRPC) DoctorReport() map[string]interface{} {
	status := Check()

	checks := make([]map[string]interface{}, len(status.Checks))
	dbHealthy := false
	for i, c := range status.Checks {
		checks[i] = map[string]interface{}{
			"name":    c.Name,
			"status":  c.Status,
			"message": c.Message,
		}
		if c.Name == "database" && c.Status == "ok" {
			dbHealthy = true
		}
	}

	return map[string]interface{}{
		"ok":         status.Status == "ok",
		"workspace":  h.workspace,
		"db_healthy": dbHealthy,
		"checks":     checks,
	}
}
