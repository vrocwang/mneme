package doctor

import "os"

type Issue struct {
	Level   string `json:"level"` // error | warning | info
	Message string `json:"message"`
}

type Report struct {
	WorkspacePath string  `json:"workspace_path"`
	Issues        []Issue `json:"issues"`
}

func Run(workspacePath string) Report {
	r := Report{WorkspacePath: workspacePath}

	if info, err := os.Stat(workspacePath); err != nil || !info.IsDir() {
		r.Issues = append(r.Issues, Issue{Level: "error", Message: "workspace directory does not exist"})
		return r
	}

	configPath := workspacePath + "/config/config.toml"
	if _, err := os.Stat(configPath); err != nil {
		r.Issues = append(r.Issues, Issue{Level: "warning", Message: "config file not found at " + configPath})
	}

	return r
}
