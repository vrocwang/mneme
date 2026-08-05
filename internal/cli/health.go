package cli

import (
	"encoding/json"
	"fmt"
)

func runHealth() error {
	core, err := bootstrap()
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}

	status := map[string]interface{}{
		"status": "ok",
		"mode":   "cli",
	}

	if core.DB != nil {
		if err := core.DB.Ping(); err != nil {
			status["database"] = map[string]string{"status": "error", "message": err.Error()}
		} else {
			status["database"] = map[string]string{"status": "ok", "message": "connected"}
		}
	} else {
		status["database"] = map[string]string{"status": "error", "message": "no database"}
	}

	if core.Provider != nil {
		status["provider"] = map[string]string{"status": "ok", "message": core.Provider.Name()}
	} else {
		status["provider"] = map[string]string{"status": "warning", "message": "no inference provider configured"}
	}

	if core.CapReg != nil {
		status["tools"] = len(core.CapReg.ToolNames())
		status["agents"] = len(core.CapReg.AllAgents())
	}

	b, _ := json.MarshalIndent(status, "", "  ")
	fmt.Println(string(b))
	return nil
}
